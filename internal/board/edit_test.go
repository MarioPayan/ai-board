package board

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func readBoard(t *testing.T, dir string) map[string]any {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, "board.json"))
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func stepAt(t *testing.T, dir string, i int) map[string]any {
	t.Helper()
	return readBoard(t, dir)["steps"].([]any)[i].(map[string]any)
}

func TestSetStep(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "board.json"), `{"title":"T","extra":{"keep":[1,2]},
"steps":[{"text":"Read spec","state":"todo","custom":"x"},{"text":"Ship it","state":"todo"},{"text":"Ship docs","state":"todo"}]}`)
	t1 := time.Date(2026, 10, 27, 10, 0, 0, 0, time.FixedZone("", -5*3600))
	t2 := t1.Add(time.Hour)
	note := "waiting on CI"

	if err := SetStep(dir, "read", "active", nil, nil, t1); err != nil {
		t.Fatal(err)
	}
	st := stepAt(t, dir, 0)
	if st["state"] != "active" || st["started"] != "2026-10-27T10:00:00-05:00" || st["ended"] != nil || st["custom"] != "x" {
		t.Errorf("active: %v", st)
	}
	// started is kept on a later move into another running state
	if err := SetStep(dir, "1", "external", &note, nil, t2); err != nil {
		t.Fatal(err)
	}
	if st = stepAt(t, dir, 0); st["started"] != "2026-10-27T10:00:00-05:00" || st["note"] != note {
		t.Errorf("external: %v", st)
	}
	empty := ""
	if err := SetStep(dir, "1", "done", &empty, nil, t2); err != nil {
		t.Fatal(err)
	}
	if st = stepAt(t, dir, 0); st["state"] != "done" || st["ended"] != "2026-10-27T11:00:00-05:00" || st["note"] != nil {
		t.Errorf("done: %v", st)
	}
	// done again keeps its ended; back to active drops it
	if err := SetStep(dir, "1", "done", nil, nil, t2.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if st = stepAt(t, dir, 0); st["ended"] != "2026-10-27T11:00:00-05:00" {
		t.Errorf("done twice: %v", st)
	}
	if err := SetStep(dir, "1", "active", nil, nil, t2); err != nil {
		t.Fatal(err)
	}
	if st = stepAt(t, dir, 0); st["ended"] != nil {
		t.Errorf("reopened step keeps ended: %v", st)
	}
	if err := SetStep(dir, "Ship", "done", nil, nil, t2); err == nil || !strings.Contains(err.Error(), "more than one") {
		t.Errorf("ambiguous prefix: %v", err)
	}
	for _, bad := range [][2]string{{"4", "done"}, {"nope", "done"}, {"1", "finished"}} {
		if err := SetStep(dir, bad[0], bad[1], nil, nil, t2); err == nil {
			t.Errorf("SetStep(%q, %q) should fail", bad[0], bad[1])
		}
	}
	if err := AddStep(dir, "Report", "waiting", t2); err != nil {
		t.Fatal(err)
	}
	if st = stepAt(t, dir, 3); st["text"] != "Report" || st["state"] != "waiting" || st["started"] != "2026-10-27T11:00:00-05:00" {
		t.Errorf("added: %v", st)
	}

	b := readBoard(t, dir)
	if extra, _ := json.Marshal(b["extra"]); string(extra) != `{"keep":[1,2]}` || b["title"] != "T" {
		t.Errorf("unknown fields lost: %v", b)
	}
	if ents, _ := os.ReadDir(dir); len(ents) != 1 {
		t.Errorf("temp files left behind: %v", ents)
	}
	// a board.json that does not parse is never overwritten
	write(t, filepath.Join(dir, "board.json"), `{"steps":`)
	if err := SetStep(dir, "1", "done", nil, nil, t2); err == nil {
		t.Error("broken board.json should fail")
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "board.json")); string(b) != `{"steps":` {
		t.Errorf("broken board.json rewritten: %s", b)
	}
}

func TestAddStepNoBoard(t *testing.T) {
	dir := t.TempDir()
	if err := AddStep(dir, "First", "todo", time.Now()); err != nil {
		t.Fatal(err)
	}
	if st := stepAt(t, dir, 0); st["started"] != nil || st["state"] != "todo" {
		t.Errorf("todo step: %v", st)
	}
}

func TestStuckSteps(t *testing.T) {
	now := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)
	ago := func(h float64) string { return now.Add(-time.Duration(h * float64(time.Hour))).Format(time.RFC3339) }
	s := &Snapshot{Board: BoardFile{Steps: []Step{
		{Text: "long active", State: "active", Started: ago(5)},
		{Text: "short active", State: "active", Started: ago(1)},
		{Text: "no start", State: "active"},
		{Text: "long ci", State: "external", Started: ago(6)},
		{Text: "long wait", State: "waiting", Started: ago(4.5)},
		{Text: "wait no start", State: "waiting"},
		{Text: "old done", State: "done", Started: ago(30)},
		{Text: "todo", State: "todo"},
	}}}
	got := strings.Join(StuckSteps(s, now, 4*time.Hour), "\n")
	want := `step 1 "long active": active for 5h00m
step 3 "no start": active with no started
step 4 "long ci": external for 6h00m
step 5 "long wait": waiting for 4h30m`
	if got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
	if got := StuckSteps(&Snapshot{}, now, time.Hour); len(got) != 0 {
		t.Errorf("empty board: %v", got)
	}
}
