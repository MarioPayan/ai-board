package board

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Archive moves into root/.archive/ every board directory under root with
// no file modified within maxAge or whose board.json does not parse, and
// every loose file there (the old one-file board format). Names that are
// keep or start with keep+"." are never touched. With dry it only reports.
// It returns one line per entry: `archived <name>  <reason>` (`would
// archive` with dry), or `skipped <name>: …`.
func Archive(root, keep string, maxAge time.Duration, now time.Time, dry bool) ([]string, error) {
	ents, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	dest := filepath.Join(root, ".archive")
	verb := "archived"
	if dry {
		verb = "would archive"
	}
	var out []string
	for _, e := range ents {
		name := e.Name()
		if strings.HasPrefix(name, ".") || (keep != "" && (name == keep || strings.HasPrefix(name, keep+"."))) {
			continue
		}
		p := filepath.Join(root, name)
		var reason string
		if !e.IsDir() {
			reason = "old format"
		} else if err := boardErr(p); err != nil {
			reason = "old format: " + err.Error()
		} else if idle := now.Sub(newest(p)); idle >= maxAge {
			reason = fmt.Sprintf("idle %dd", int(idle.Hours()/24))
		}
		if reason == "" {
			continue
		}
		if !dry {
			if _, err := os.Lstat(filepath.Join(dest, name)); err == nil {
				out = append(out, "skipped "+name+": already in .archive")
				continue
			}
			if err := os.MkdirAll(dest, 0o755); err != nil {
				return out, err
			}
			if err := os.Rename(p, filepath.Join(dest, name)); err != nil {
				return out, err
			}
		}
		out = append(out, verb+" "+name+"  "+reason)
	}
	return out, nil
}

// boardErr is the parse error of dir/board.json; a missing file is fine.
func boardErr(dir string) error {
	b, err := os.ReadFile(filepath.Join(dir, "board.json"))
	if err != nil {
		return nil
	}
	var bf BoardFile
	return json.Unmarshal(b, &bf)
}

// newest is the latest mtime of dir or anything under it.
func newest(dir string) time.Time {
	var t time.Time
	_ = filepath.Walk(dir, func(_ string, fi os.FileInfo, err error) error {
		if err == nil && fi.ModTime().After(t) {
			t = fi.ModTime()
		}
		return nil
	})
	return t
}
