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

// fixtures maps each fixture to the fixed clock it is rendered at.
var fixtures = map[string]string{
	"disk-cleanup": "2026-10-26T11:58:00-05:00",
	"agents-demo":  "2026-10-26T14:00:00-05:00",
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
	for name, now := range fixtures {
		t.Run(name, func(t *testing.T) {
			clock, _ := time.Parse(time.RFC3339, now)
			dir := copyFixture(t, filepath.Join("..", "..", "testdata", name), clock.Add(-3*time.Minute))
			var out bytes.Buffer
			if err := cmdView([]string{"--once", "--width", "80", "--now", now, dir}, &out); err != nil {
				t.Fatal(err)
			}
			if bytes.Contains(out.Bytes(), []byte("\x1b")) {
				t.Error("--once output contains escape sequences")
			}
			golden := filepath.Join("..", "..", "testdata", name+".golden")
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
	if want := "waiting for " + dir + "\n"; out.String() != want {
		t.Errorf("got %q want %q", out.String(), want)
	}
}
