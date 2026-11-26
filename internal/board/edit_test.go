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

func TestSetStatus(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "board.json"), `{"title":"T","status":{"state":"working","text":"old","extra":1},"steps":[{"text":"a","state":"todo"}]}`)
	if err := SetStatus(dir, "waiting", "Your pick"); err != nil {
		t.Fatal(err)
	}
	b := readBoard(t, dir)
	st := b["status"].(map[string]any)
	if st["state"] != "waiting" || st["text"] != "Your pick" || st["extra"] != 1.0 {
		t.Errorf("status: %v", st)
	}
	if b["title"] != "T" || len(b["steps"].([]any)) != 1 {
		t.Errorf("other fields lost: %v", b)
	}
	for _, bad := range [][2]string{{"finished", "x"}, {"done", ""}} {
		if err := SetStatus(dir, bad[0], bad[1]); err == nil {
			t.Errorf("SetStatus(%q, %q) should fail", bad[0], bad[1])
		}
	}
	if ents, _ := os.ReadDir(dir); len(ents) != 1 {
		t.Errorf("temp files left behind: %v", ents)
	}
	// a board.json that does not parse is never overwritten
	write(t, filepath.Join(dir, "board.json"), `{"status":`)
	if err := SetStatus(dir, "done", "x"); err == nil {
		t.Error("broken board.json should fail")
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "board.json")); string(b) != `{"status":` {
		t.Errorf("broken board.json rewritten: %s", b)
	}
	// no board.json yet: created with the status
	dir = t.TempDir()
	if err := SetStatus(dir, "done", "Shipped"); err != nil {
		t.Fatal(err)
	}
	if st := readBoard(t, dir)["status"].(map[string]any); st["state"] != "done" || st["text"] != "Shipped" {
		t.Errorf("new board status: %v", st)
	}
}

func TestPending(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "board.json"), `{"title":"T","pending":[{"text":"Review spec","link":"https://x","custom":"y"}]}`)
	for _, a := range [][2]string{{"Answer deck", ""}, {"Approve deploy", "https://ci"}, {"Third", ""}} {
		if err := AddPending(dir, a[0], a[1]); err != nil {
			t.Fatal(err)
		}
	}
	items := func() []any { return readBoard(t, dir)["pending"].([]any) }
	p := items()
	if len(p) != 4 || p[0].(map[string]any)["custom"] != "y" {
		t.Fatalf("pending: %v", p)
	}
	if it := p[1].(map[string]any); it["text"] != "Answer deck" || it["link"] != nil {
		t.Errorf("added without link: %v", it)
	}
	if it := p[2].(map[string]any); it["link"] != "https://ci" {
		t.Errorf("added with link: %v", it)
	}
	if err := RmPending(dir, "2"); err != nil {
		t.Fatal(err)
	}
	if err := RmPending(dir, "Approve deploy"); err != nil {
		t.Fatal(err)
	}
	p = items()
	if len(p) != 2 || p[0].(map[string]any)["text"] != "Review spec" || p[1].(map[string]any)["text"] != "Third" {
		t.Errorf("after rm: %v", p)
	}
	// exact text only, and indexes in range
	for _, bad := range []string{"third", "Thi", "0", "3", "nope"} {
		if err := RmPending(dir, bad); err == nil {
			t.Errorf("RmPending(%q) should fail", bad)
		}
	}
	if err := AddPending(dir, "", ""); err == nil {
		t.Error("empty pending text should fail")
	}
	if b := readBoard(t, dir); b["title"] != "T" || len(b["pending"].([]any)) != 2 {
		t.Errorf("failed edits changed the board: %v", b)
	}
	if ents, _ := os.ReadDir(dir); len(ents) != 1 {
		t.Errorf("temp files left behind: %v", ents)
	}
}
