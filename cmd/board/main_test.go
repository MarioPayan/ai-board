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
