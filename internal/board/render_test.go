package board

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
)

func TestStepDuration(t *testing.T) {
	cases := []struct {
		started, ended, want string
	}{
		{"2026-10-26T11:05:00-05:00", "2026-10-26T11:14:00-05:00", "9m"},
		{"2026-10-26T11:05:00-05:00", "2026-10-26T11:05:42-05:00", "42s"},
		{"2026-10-26T11:00:00-05:00", "2026-10-26T12:30:00-05:00", "1h30m"},
		{"2026-10-26T16:05:00Z", "2026-10-26T11:14:00-05:00", "9m"}, // mixed zones
		{"", "2026-10-26T11:14:00-05:00", ""},
		{"2026-10-26T11:05:00-05:00", "", ""},
		{"garbage", "2026-10-26T11:14:00-05:00", ""},
		{"2026-10-26T11:14:00-05:00", "2026-10-26T11:05:00-05:00", "0s"}, // ended before started
	}
	for _, c := range cases {
		if got := stepDur(Step{Started: c.started, Ended: c.ended}); got != c.want {
			t.Errorf("stepDur(%q, %q) = %q, want %q", c.started, c.ended, got, c.want)
		}
	}
}

func TestFmtDur(t *testing.T) {
	cases := map[time.Duration]string{
		-time.Second:                    "0s",
		59 * time.Second:                "59s",
		9 * time.Minute:                 "9m",
		59*time.Minute + 59*time.Second: "59m",
		time.Hour:                       "1h00m",
		26*time.Hour + 5*time.Minute:    "26h05m",
	}
	for d, want := range cases {
		if got := fmtDur(d); got != want {
			t.Errorf("fmtDur(%v) = %q, want %q", d, got, want)
		}
	}
}

func TestStale(t *testing.T) {
	now := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		state string
		age   time.Duration
		want  bool
	}{
		{"working", 11 * time.Minute, true},
		{"working", 10 * time.Minute, false}, // more than 10 minutes, not equal
		{"working", 9 * time.Minute, false},
		{"waiting", time.Hour, false},
		{"done", time.Hour, false},
		{"blocked", time.Hour, false},
	}
	for _, c := range cases {
		s := &Snapshot{Status: Status{State: c.state}, Updated: now.Add(-c.age)}
		if got := isStale(s, now); got != c.want {
			t.Errorf("isStale(%s, %v) = %v, want %v", c.state, c.age, got, c.want)
		}
	}
	if isStale(&Snapshot{Status: Status{State: "working"}}, now) {
		t.Error("isStale with no mtime should be false")
	}
}

// Glamour's standard styles pad inline code with a space on each side.
func TestInlineCodeNoPadding(t *testing.T) {
	sec := Section{Name: "10-x.md", Title: "X", Kind: "md", Body: []byte("Viewer: `~/repos/Kaze/board` (main)\n")}
	for _, r := range []*Renderer{{}, {Color: true, Dark: true}, {Color: true}} {
		r.init()
		got := ansi.Strip(strings.Join(r.markdown(sec, 80), "\n"))
		if !strings.Contains(got, "Viewer: ~/repos/Kaze/board (main)") {
			t.Errorf("Color=%v Dark=%v: inline code is padded: %q", r.Color, r.Dark, got)
		}
	}
}

// Inline code is cyan (ANSI 6) with no background in both live styles, not
// Glamour's red on grey.
func TestInlineCodeCyan(t *testing.T) {
	sec := Section{Name: "10-x.md", Title: "X", Kind: "md", Body: []byte("run `go test` now\n")}
	for _, dark := range []bool{true, false} {
		r := &Renderer{Color: true, Dark: dark}
		r.init()
		got := strings.Join(r.markdown(sec, 80), "\n")
		i := strings.Index(got, "go test")
		if i < 0 {
			t.Fatalf("dark=%v: no code in %q", dark, got)
		}
		// the escape sequence right before the code text
		pre := got[strings.LastIndex(got[:i], "\x1b["):i]
		if !strings.Contains(pre, "36") || strings.Contains(pre, "38;5;203") || strings.Contains(pre, "48;5;") {
			t.Errorf("dark=%v: inline code styled %q, want cyan (36) and no background", dark, pre)
		}
	}
}

// The header separator must not run under a bar that has no header text.
func TestTableSeparatorUnderHeadersOnly(t *testing.T) {
	tb := &Table{
		Columns: []Column{{Name: "Zone"}, {Name: "Size", Align: "right", Bar: true}},
		Rows:    [][]Cell{{{Text: "a"}, {Text: "10G", V: 10, HasV: true}}, {{Text: "bb"}, {Text: "5G", V: 5, HasV: true}}},
	}
	r := &Renderer{}
	r.init()
	lines := r.table(tb, 60)
	head, sep := lines[0], lines[1]
	if ansi.StringWidth(sep) > ansi.StringWidth(head) {
		t.Errorf("separator wider than header:\n%q\n%q", head, sep)
	}
	hr, sr := []rune(head), []rune(sep)
	for i, c := range sr {
		if c == '─' && (i >= len(hr) || hr[i] == ' ') {
			t.Errorf("separator cell %d has no header above it:\n%s\n%s", i, head, sep)
			break
		}
	}
}

func TestFitStep(t *testing.T) {
	text40 := strings.Repeat("t", 40)
	note30 := strings.Repeat("n", 30)
	cases := []struct {
		name, text, note string
		room             int
		wantText         string
		wantNote         string
	}{
		{"fits", "short", "note", 20, "short", "note"},
		{"note shrinks first", "0123456789", note30, 25, "0123456789", strings.Repeat("n", 11) + "…"},
		{"then text, note kept at 6", text40, note30, 30, strings.Repeat("t", 20) + "…", "nnnnn…"},
		{"text never below 20: note dropped", text40, note30, 25, strings.Repeat("t", 24) + "…", ""},
		{"no note", text40, "", 10, strings.Repeat("t", 9) + "…", ""},
	}
	for _, c := range cases {
		gt, gn := fitStep(c.text, c.note, c.room)
		if gt != c.wantText || gn != c.wantNote {
			t.Errorf("%s: fitStep = %q, %q; want %q, %q", c.name, gt, gn, c.wantText, c.wantNote)
		}
		if gn != "" && ansi.StringWidth(gt+noteSep+gn) > c.room {
			t.Errorf("%s: %q overflows %d cells", c.name, gt+noteSep+gn, c.room)
		}
	}
}

func TestCollapseFinished(t *testing.T) {
	s := &Snapshot{Status: Status{State: "working"}, Board: BoardFile{Steps: []Step{
		{Text: "one", State: "done", Started: "2026-10-26T10:00:00Z", Ended: "2026-10-26T10:05:00Z"},
		{Text: "latest", State: "done", Started: "2026-10-26T10:05:00Z", Ended: "2026-10-26T10:30:00Z"},
		{Text: "three", State: "done", Started: "2026-10-26T10:10:00Z", Ended: "2026-10-26T10:20:00Z"},
		{Text: "skipped", State: "skipped"},
		{Text: "now", State: "active", Started: "2026-10-26T10:30:00Z"},
	}}}
	now := time.Date(2026, 10, 26, 10, 40, 0, 0, time.UTC)
	got := Once(s, 60, now, false)
	if !strings.Contains(got, "✔  4 · last: latest") || !strings.Contains(got, "25m\n") || strings.Contains(got, "three") {
		t.Errorf("collapsed plan:\n%s", got)
	}
	exp := Once(s, 60, now, true)
	for _, w := range []string{"one", "latest", "three", "skipped"} {
		if !strings.Contains(exp, w) {
			t.Errorf("expanded plan misses %q:\n%s", w, exp)
		}
	}
	s.Board.Steps = s.Board.Steps[1:] // three finished: shown one by one
	if got := Once(s, 60, now, false); strings.Contains(got, "· last:") {
		t.Errorf("3 finished steps should not collapse:\n%s", got)
	}
}

// One blank line separates the fixed area, Plan, Details and Log; none
// between a subsection title and its body.
func TestBlankLinesBetweenSections(t *testing.T) {
	s := &Snapshot{
		Status:   Status{State: "working", Text: "x"},
		Board:    BoardFile{Title: "T", Steps: []Step{{Text: "s", State: "todo"}}},
		Sections: []Section{{Name: "10-a.md", Title: "A", Kind: "md", Body: []byte("alpha\n")}},
		Log:      []LogEntry{{Text: "entry"}},
	}
	got := Once(s, 40, time.Now(), false)
	lines := strings.Split(got, "\n")
	idx := func(prefix string) int {
		for i, l := range lines {
			if strings.HasPrefix(l, prefix) {
				return i
			}
		}
		t.Fatalf("no line starting %q in\n%s", prefix, got)
		return -1
	}
	for _, h := range []string{"📋 Plan", "🔎 Details", "📜 Log"} {
		i := idx(h)
		if lines[i-1] != "" || (i > 1 && lines[i-2] == "") {
			t.Errorf("want exactly one blank line before %q:\n%s", h, got)
		}
	}
	if i := idx("A"); lines[i+1] == "" || lines[i-1] == "" {
		t.Errorf("subsection title should sit between its heading and body:\n%s", got)
	}
}
