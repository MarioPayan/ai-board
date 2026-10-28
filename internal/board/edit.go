package board

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
)

// obj is a JSON object kept as raw values, so fields this package does not
// know survive a rewrite byte for byte.
type obj = map[string]json.RawMessage

// EditBoard reads dir/board.json (an empty object when missing), lets fn
// change its top-level fields and replaces the file atomically (temp file,
// rename). Fields fn does not touch are kept as they were.
func EditBoard(dir string, fn func(b obj) error) error {
	p := filepath.Join(dir, "board.json")
	b := obj{}
	if data, err := os.ReadFile(p); err == nil {
		if err := json.Unmarshal(data, &b); err != nil {
			return fmt.Errorf("board.json: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	if b == nil {
		b = obj{}
	}
	if err := fn(b); err != nil {
		return err
	}
	out, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".board.json.*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name()) // no-op after the rename
	if _, err := f.Write(append(out, '\n')); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Chmod(f.Name(), 0o644); err != nil {
		return err
	}
	return os.Rename(f.Name(), p)
}

func list(b obj, key string) ([]obj, error) {
	var l []obj
	if raw, ok := b[key]; ok && string(raw) != "null" {
		if err := json.Unmarshal(raw, &l); err != nil {
			return nil, fmt.Errorf("board.json %s: %w", key, err)
		}
	}
	return l, nil
}

func setList(b obj, key string, l []obj) {
	if l == nil {
		l = []obj{}
	}
	set(b, key, l)
}

func set(o obj, k string, v any) {
	raw, _ := json.Marshal(v) // strings, numbers and objects of them
	o[k] = raw
}

// setOrDelete sets a string field, or removes it when v is empty.
func setOrDelete(o obj, k, v string) {
	if v == "" {
		delete(o, k)
	} else {
		set(o, k, v)
	}
}

func str(o obj, k string) string {
	var s string
	_ = json.Unmarshal(o[k], &s)
	return s
}

// StepStates are the allowed steps[].state values.
var StepStates = []string{"todo", "active", "waiting", "external", "failed", "done", "skipped"}

func checkEnum(field, v string, allowed []string) error {
	if !slices.Contains(allowed, v) {
		return fmt.Errorf("%s %q: want one of %s", field, v, strings.Join(allowed, ", "))
	}
	return nil
}

// stamp sets step st to state: entering active, waiting or external sets
// `started` when it is unset; entering done, failed or skipped sets `ended`
// (kept when the step was already in that state); any unfinished state
// drops `ended`.
func stamp(st obj, state string, now time.Time) {
	ts := now.Format(time.RFC3339)
	switch state {
	case "done", "failed", "skipped":
		if str(st, "state") != state || str(st, "ended") == "" {
			set(st, "ended", ts)
		}
	default:
		if state != "todo" && str(st, "started") == "" {
			set(st, "started", ts)
		}
		delete(st, "ended")
	}
	set(st, "state", state)
}

// findStep resolves sel, a 1-based index or a case-insensitive prefix of
// exactly one step's text.
func findStep(steps []obj, sel string) (int, error) {
	if n, err := strconv.Atoi(sel); err == nil {
		if n < 1 || n > len(steps) {
			return 0, fmt.Errorf("no step %d (%d steps)", n, len(steps))
		}
		return n - 1, nil
	}
	found := -1
	for i, st := range steps {
		if strings.HasPrefix(strings.ToLower(str(st, "text")), strings.ToLower(sel)) {
			if found >= 0 {
				return 0, fmt.Errorf("%q matches more than one step", sel)
			}
			found = i
		}
	}
	if found < 0 {
		return 0, fmt.Errorf("no step starts with %q", sel)
	}
	return found, nil
}

// SetStep moves the step sel names (see findStep) to state, stamping
// started/ended. A nil note or link leaves it alone; "" removes it.
func SetStep(dir, sel, state string, note, link *string, now time.Time) error {
	if err := checkEnum("state", state, StepStates); err != nil {
		return err
	}
	return EditBoard(dir, func(b obj) error {
		steps, err := list(b, "steps")
		if err != nil {
			return err
		}
		i, err := findStep(steps, sel)
		if err != nil {
			return err
		}
		stamp(steps[i], state, now)
		if note != nil {
			setOrDelete(steps[i], "note", *note)
		}
		if link != nil {
			setOrDelete(steps[i], "link", *link)
		}
		setList(b, "steps", steps)
		return nil
	})
}

// AddStep appends a step in state.
func AddStep(dir, text, state string, now time.Time) error {
	if err := checkEnum("state", state, StepStates); err != nil {
		return err
	}
	return EditBoard(dir, func(b obj) error {
		steps, err := list(b, "steps")
		if err != nil {
			return err
		}
		st := obj{}
		set(st, "text", text)
		stamp(st, state, now)
		setList(b, "steps", append(steps, st))
		return nil
	})
}

// StuckSteps lists steps that look stuck, one line each: active, waiting or
// external for more than maxAge since `started`, and active with no
// `started`.
func StuckSteps(s *Snapshot, now time.Time, maxAge time.Duration) []string {
	var out []string
	for i, st := range s.Board.Steps {
		if st.State != "active" && st.State != "waiting" && st.State != "external" {
			continue
		}
		id := fmt.Sprintf("step %d %s", i+1, quote(st.Text))
		t, err := time.Parse(time.RFC3339, st.Started)
		switch {
		case err != nil && st.State == "active":
			out = append(out, id+": active with no started")
		case err == nil && now.Sub(t) > maxAge:
			out = append(out, fmt.Sprintf("%s: %s for %s", id, st.State, fmtDur(now.Sub(t))))
		}
	}
	return out
}
