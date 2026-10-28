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
	dir := t.TempDir()
	started := time.Now().Add(-3 * time.Hour).Format(time.RFC3339)
	board := `{"steps":[{"text":"build","state":"active","started":"` + started + `"},{"text":"x","state":"active"}]}`
	if err := os.WriteFile(filepath.Join(dir, "board.json"), []byte(board), 0o644); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) string {
		var out bytes.Buffer
		if err := cmdCheck(args, &out); err != nil {
			t.Fatal(err)
		}
		return out.String()
	}
	if got := run("--dir", dir); got != "step 2 \"x\": active with no started\n" {
		t.Errorf("default 4h: %q", got)
	}
	if got := run("--dir", dir, "--hours", "2"); !strings.HasPrefix(got, "step 1 \"build\": active for 3h00m\n") {
		t.Errorf("--hours 2: %q", got)
	}
	t.Setenv("CLAUDE_CODE_SESSION_ID", "")
	if got := run(); got != "" {
		t.Errorf("no session: %q", got)
	}
	if got := run("--dir", filepath.Join(dir, "missing")); got != "" {
		t.Errorf("missing board: %q", got)
	}
	if err := cmdCheck([]string{"--hours", "x"}, &bytes.Buffer{}); err == nil {
		t.Error("--hours x should fail")
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
