package main

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"testing"
	"time"
)

var update = flag.Bool("update", false, "rewrite testdata/*.golden")

// goldenCase renders fixture at a fixed clock, with every file's mtime set
// age before it, into testdata/<name>.golden.
type goldenCase struct {
	name, fixture, now string
	age                time.Duration
	args               []string
}

var cases = []goldenCase{
	{"disk-cleanup", "disk-cleanup", "2026-10-26T11:58:00-05:00", 3 * time.Minute, nil}, // empty pending
	{"agents-demo", "agents-demo", "2026-10-26T14:00:00-05:00", 3 * time.Minute, nil},
	{"agents-many", "agents-many", "2026-10-26T14:00:00-05:00", 3 * time.Minute, nil},                      // 8 agents: 3 shown, +5 more
	{"agents-many-all", "agents-many", "2026-10-26T14:00:00-05:00", 3 * time.Minute, []string{"--agents"}}, // every agent, in the body
	{"agents-demo-legend", "agents-demo", "2026-10-26T14:00:00-05:00", 3 * time.Minute, []string{"--legend"}},
	{"checks-fail-legend", "checks-fail", "2026-10-26T15:30:00-05:00", 3 * time.Minute, []string{"--legend"}},
	{"done-heavy", "done-heavy", "2026-10-26T10:28:00-05:00", 3 * time.Minute, nil}, // >3 finished: collapsed
	{"done-heavy-expand", "done-heavy", "2026-10-26T10:28:00-05:00", 3 * time.Minute, []string{"--expand"}},
	{"done-heavy-stale", "done-heavy", "2026-10-26T10:28:00-05:00", 25 * time.Minute, nil}, // working, no write for 25m
	{"all-done", "all-done", "2026-10-26T09:00:00-05:00", 3 * time.Minute, nil},            // status done
	{"checks-pass", "checks-pass", "2026-10-26T15:30:00-05:00", 3 * time.Minute, nil},
	{"checks-fail", "checks-fail", "2026-10-26T15:30:00-05:00", 3 * time.Minute, nil},                      // only failing checks
	{"checks-fail-all", "checks-fail", "2026-10-26T15:30:00-05:00", 3 * time.Minute, []string{"--checks"}}, // every check
	{"prs-running", "prs-running", "2026-10-27T11:00:00-05:00", 3 * time.Minute, nil},
}

// copyFixture copies a fixture into a temp dir and pins every mtime to
// mtime, so `updated <ago>` is deterministic.
func copyFixture(t *testing.T, src string, mtime time.Time) string {
	t.Helper()
	dst := filepath.Join(t.TempDir(), filepath.Base(src))
	err := filepath.Walk(src, func(p string, fi os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		out := filepath.Join(dst, rel)
		if fi.IsDir() {
			return os.MkdirAll(out, 0o755)
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(out, b, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = filepath.Walk(dst, func(p string, _ os.FileInfo, _ error) error {
		return os.Chtimes(p, mtime, mtime)
	})
	return dst
}

func TestGolden(t *testing.T) {
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			clock, err := time.Parse(time.RFC3339, c.now)
			if err != nil {
				t.Fatal(err)
			}
			dir := copyFixture(t, filepath.Join("..", "..", "testdata", c.fixture), clock.Add(-c.age))
			var out bytes.Buffer
			args := append([]string{"--once", "--width", "80", "--now", c.now}, c.args...)
			if err := cmdView(append(args, dir), &out); err != nil {
				t.Fatal(err)
			}
			if bytes.Contains(out.Bytes(), []byte("\x1b")) {
				t.Error("--once output contains escape sequences")
			}
			golden := filepath.Join("..", "..", "testdata", c.name+".golden")
			if *update {
				if err := os.WriteFile(golden, out.Bytes(), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			want, err := os.ReadFile(golden)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(out.Bytes(), want) {
				t.Errorf("render differs from %s (run go test ./cmd/board -update)\n--- got\n%s\n--- want\n%s", golden, out.String(), want)
			}
		})
	}
}

func TestMissingDir(t *testing.T) {
	var out bytes.Buffer
	dir := filepath.Join(t.TempDir(), "nope")
	if err := cmdView([]string{"--once", dir}, &out); err != nil {
		t.Fatal(err)
	}
	if want := "⏳ waiting for " + dir + "\n"; out.String() != want {
		t.Errorf("got %q want %q", out.String(), want)
	}
}
