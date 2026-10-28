package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCheckCmd(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, ".claude", "boards", "s1")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	started := time.Now().Add(-3 * time.Hour).Format(time.RFC3339)
	board := `{"steps":[{"text":"build","state":"active","started":"` + started + `"},{"text":"x","state":"active"}]}`
	if err := os.WriteFile(filepath.Join(dir, "board.json"), []byte(board), 0o644); err != nil {
		t.Fatal(err)
	}
	// run returns the output; the count it returns (exit 1 when > 0) must
	// match the lines printed
	run := func(args ...string) string {
		var out bytes.Buffer
		n, err := cmdCheck(args, &out)
		if err != nil {
			t.Fatal(err)
		}
		if lines := strings.Count(out.String(), "\n"); n != lines {
			t.Errorf("%v: returned %d problems, printed %d lines", args, n, lines)
		}
		return out.String()
	}
	if got := run("--dir", dir); got != "step 2 \"x\": active with no started\n" {
		t.Errorf("default 4h: %q", got)
	}
	if got := run("--dir", dir, "--hours", "2"); !strings.HasPrefix(got, "step 1 \"build\": active for 3h00m\n") {
		t.Errorf("--hours 2: %q", got)
	}
	t.Setenv("CLAUDE_CODE_SESSION_ID", "s1") // no --dir: the session's board
	if got := run("--hours", "2"); strings.Count(got, "\n") != 2 {
		t.Errorf("session board: %q", got)
	}
	t.Setenv("CLAUDE_CODE_SESSION_ID", "")
	if got := run(); got != "" {
		t.Errorf("no session: %q", got)
	}
	if got := run("--dir", filepath.Join(dir, "missing")); got != "" {
		t.Errorf("missing board: %q", got)
	}
	if n, err := cmdCheck([]string{"--hours", "x"}, &bytes.Buffer{}); err == nil || n != 0 {
		t.Error("--hours x should fail with no problems counted")
	}
}

func TestArchiveCmd(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CODE_SESSION_ID", "live")
	root := filepath.Join(home, ".claude", "boards")
	old := time.Now().Add(-10 * 24 * time.Hour)
	mk := func(name, board string, mtime time.Time) {
		p := filepath.Join(root, name)
		if strings.HasSuffix(name, ".md") {
			if err := os.MkdirAll(root, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(p, []byte("# old"), 0o644); err != nil {
				t.Fatal(err)
			}
		} else {
			if err := os.MkdirAll(filepath.Join(p, "sections"), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(p, "board.json"), []byte(board), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		_ = filepath.Walk(p, func(f string, _ os.FileInfo, _ error) error { return os.Chtimes(f, mtime, mtime) })
	}
	mk("idle", `{"title":"x"}`, old)
	mk("fresh", `{"title":"x"}`, time.Now())
	mk("oldfmt", `{"steps":["a string step"]}`, time.Now())
	mk("live", `{"title":"x"}`, old) // the current session: never touched
	mk("live.md", "", old)           // its old-format file too
	mk("gone.md", "", time.Now())    // loose file: old format whatever its age
	run := func(args ...string) string {
		var out bytes.Buffer
		if err := cmdArchive(args, &out); err != nil {
			t.Fatal(err)
		}
		return out.String()
	}
	want := "would archive gone.md  old format\nwould archive idle  idle 10d\nwould archive oldfmt  old format: json: cannot unmarshal string into Go struct field BoardFile.steps of type board.Step\n"
	if got := run("--dry-run"); got != want {
		t.Errorf("dry run:\n%s\nwant\n%s", got, want)
	}
	if _, err := os.Stat(filepath.Join(root, ".archive")); !os.IsNotExist(err) {
		t.Error("--dry-run created .archive")
	}
	if got := run("--days", "30"); got != "archived gone.md  old format\narchived oldfmt  old format: json: cannot unmarshal string into Go struct field BoardFile.steps of type board.Step\n" {
		t.Errorf("--days 30:\n%s", got)
	}
	if got := run(); got != "archived idle  idle 10d\n" {
		t.Errorf("second run:\n%s", got)
	}
	ents, _ := os.ReadDir(root)
	var names []string
	for _, e := range ents {
		names = append(names, e.Name())
	}
	if strings.Join(names, " ") != ".archive fresh live live.md" {
		t.Errorf("left in root: %v", names)
	}
	if _, err := os.Stat(filepath.Join(root, ".archive", "idle", "board.json")); err != nil {
		t.Error("idle board not moved whole:", err)
	}
}

func TestPRCmd(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "board.json"), []byte(`{"title":"T","mine":true}`), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"set", "kazemu/board#12", "--title", "feat: x", "--ci", "pending", "--dir", dir},
		{"set", "kazemu/board#12", "--ci", "pass", "--review", "approved", "--dir", dir}, // upsert keeps the title
		{"set", "o/r#3", "--state", "draft", "--dir", dir},
		{"rm", "o/r#3", "--dir", dir},
	} {
		if err := cmdPR(args); err != nil {
			t.Fatalf("%v: %v", args, err)
		}
	}
	b, _ := os.ReadFile(filepath.Join(dir, "board.json"))
	got := string(b)
	for _, want := range []string{`"mine": true`, `"repo": "kazemu/board"`, `"n": 12`, `"title": "feat: x"`, `"ci": "pass"`, `"review": "approved"`} {
		if !strings.Contains(got, want) {
			t.Errorf("board.json misses %s:\n%s", want, got)
		}
	}
	if strings.Contains(got, "o/r") {
		t.Errorf("rm left o/r#3:\n%s", got)
	}
	for _, bad := range [][]string{
		{"set", "norepo", "--dir", dir},
		{"set", "o/r#3", "--ci", "green", "--dir", dir},
		{"rm", "o/r#3", "--dir", dir}, // already gone
		{"rm", "o/r#3", "--title", "x", "--dir", dir},
	} {
		if err := cmdPR(bad); err == nil {
			t.Errorf("%v should fail", bad)
		}
	}
}

func TestRunCmd(t *testing.T) {
	dir := t.TempDir()
	for _, args := range [][]string{
		{"set", "web", "--url", "http://localhost:3000", "--cmd", "pnpm dev", "--dir", dir},
		{"set", "web", "--login", "user in 1Password: Foo", "--dir", dir},
		{"set", "api", "server", "--dir", dir}, // names may have spaces
		{"rm", "api", "server", "--dir", dir},
	} {
		if err := cmdRun(args); err != nil {
			t.Fatalf("%v: %v", args, err)
		}
	}
	b, _ := os.ReadFile(filepath.Join(dir, "board.json"))
	got := string(b)
	for _, want := range []string{`"name": "web"`, `"url": "http://localhost:3000"`, `"cmd": "pnpm dev"`, `"login": "user in 1Password: Foo"`} {
		if !strings.Contains(got, want) {
			t.Errorf("board.json misses %s:\n%s", want, got)
		}
	}
	if strings.Contains(got, "api") {
		t.Errorf("rm left api server:\n%s", got)
	}
	if err := cmdRun([]string{"rm", "nope", "--dir", dir}); err == nil {
		t.Error("rm of a missing entry should fail")
	}
}

func TestStepCmd(t *testing.T) {
	dir := t.TempDir()
	for _, args := range [][]string{
		{"add", "Write", "tests", "--dir", dir},
		{"add", "--state", "active", "Ship", "--dir", dir},
		{"write", "done", "--note", "all green", "--dir", dir},
	} {
		if err := cmdStep(args); err != nil {
			t.Fatalf("%v: %v", args, err)
		}
	}
	b, _ := os.ReadFile(filepath.Join(dir, "board.json"))
	for _, want := range []string{`"text": "Write tests"`, `"state": "done"`, `"note": "all green"`, `"text": "Ship"`, `"state": "active"`} {
		if !strings.Contains(string(b), want) {
			t.Errorf("board.json misses %s:\n%s", want, b)
		}
	}
	if err := cmdStep([]string{"1", "--dir", dir}); err == nil {
		t.Error("missing state should fail")
	}
}
