package board

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
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
	got := Once(s, 60, now, Options{})
	if !strings.Contains(got, "✔  4 · last: latest") || !strings.Contains(got, "25m\n") || strings.Contains(got, "three") {
		t.Errorf("collapsed plan:\n%s", got)
	}
	exp := Once(s, 60, now, Options{Expand: true})
	for _, w := range []string{"one", "latest", "three", "skipped"} {
		if !strings.Contains(exp, w) {
			t.Errorf("expanded plan misses %q:\n%s", w, exp)
		}
	}
	s.Board.Steps = s.Board.Steps[1:] // three finished: shown one by one
	if got := Once(s, 60, now, Options{}); strings.Contains(got, "· last:") {
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
	got := Once(s, 40, time.Now(), Options{})
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
	// the fixed area ends with its rule, right above the first block
	if i := idx("📋 Plan"); lines[i-1] != strings.Repeat("─", 40) || lines[i-2] == "" {
		t.Errorf("want the rule and no blank line before the plan:\n%s", got)
	}
	for _, h := range []string{"🔎 Details", "📜 Log"} {
		i := idx(h)
		if lines[i-1] != "" || (i > 1 && lines[i-2] == "") {
			t.Errorf("want exactly one blank line before %q:\n%s", h, got)
		}
	}
	if i := idx("A"); lines[i+1] == "" || lines[i-1] == "" {
		t.Errorf("subsection title should sit between its heading and body:\n%s", got)
	}
}

func TestChecksSummary(t *testing.T) {
	pass := &Checks{Checks: []Check{{Name: "a", OK: true}, {Name: "b", OK: true, Detail: "8 tests"}}}
	fail := &Checks{Checks: []Check{{Name: "a", OK: true}, {Name: "b", OK: false, Detail: "boom"}, {Name: "c", OK: false}}}
	r := &Renderer{}
	r.init()
	if got := r.checks(pass, 60).lines; len(got) != 1 || got[0] != "✔ 2/2 checks" {
		t.Errorf("all pass: %q", got)
	}
	want := []string{"✘ 2/3 checks failed", "  ✘ b — boom", "  ✘ c"}
	if got := r.checks(fail, 60).lines; strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("some fail: %q, want %q", got, want)
	}
	r.AllChecks = true
	want = []string{"✘ 2/3 checks failed", "  ✔ a", "  ✘ b — boom", "  ✘ c"}
	if got := r.checks(fail, 60).lines; strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("all checks: %q, want %q", got, want)
	}
	if got := r.checks(pass, 60).lines; len(got) != 3 {
		t.Errorf("all checks, all pass: %q", got)
	}
}

func TestSortAgents(t *testing.T) {
	now := time.Date(2026, 10, 26, 14, 0, 0, 0, time.UTC)
	at := func(min int) time.Time { return now.Add(-time.Duration(min) * time.Minute) }
	in := []Agent{
		{ID: "short", Started: at(5)},
		{ID: "long", Started: at(50)},
		{ID: "over-small", Started: at(25), ExpectMin: 20},
		{ID: "failed-new", Started: at(2), Failed: true},
		{ID: "within", Started: at(40), ExpectMin: 60},
		{ID: "over-big", Started: at(45), ExpectMin: 10},
		{ID: "failed-old", Started: at(30), Failed: true},
	}
	var got []string
	for _, a := range sortAgents(in, now) {
		got = append(got, a.ID)
	}
	want := "failed-old failed-new over-big over-small long within short"
	if strings.Join(got, " ") != want {
		t.Errorf("sortAgents = %v, want %s", got, want)
	}
	if in[0].ID != "short" {
		t.Error("sortAgents must not reorder its input")
	}
}

func TestAgentsCapped(t *testing.T) {
	now := time.Date(2026, 10, 26, 14, 0, 0, 0, time.UTC)
	at := func(min int) time.Time { return now.Add(-time.Duration(min) * time.Minute) }
	r := &Renderer{}
	r.init()
	mk := func(n, failed, over int) *Snapshot {
		s := &Snapshot{}
		for i := 0; i < n; i++ {
			a := Agent{ID: fmt.Sprint(i), Name: fmt.Sprintf("agent %d", i), Started: at(10 + i)}
			switch {
			case i < failed:
				a.Failed = true
			case i < failed+over:
				a.ExpectMin = 5
			}
			s.Agents = append(s.Agents, a)
		}
		return s
	}
	// the header slots show the first three, sorted, and +N for the rest;
	// a lists every agent
	cases := []struct {
		n, failed, over int
		all             bool
		lines           int
		first, summary  string
	}{
		{3, 0, 0, false, 3, "🤖  agent 2", ""},    // fits: no summary
		{8, 1, 2, false, 3, "🔴  agent 0", " +5"}, // failed first, over ones shown, none hidden
		{6, 2, 3, false, 3, "🔴  agent 1", " +3"}, // 2 over pushed below the cut
		{8, 1, 2, true, 8, "🔴 agent 0", ""},      // all
	}
	for _, c := range cases {
		var got []string
		if c.all {
			got = r.agents(mk(c.n, c.failed, c.over), 80, now).lines
		} else {
			got = r.slots(mk(c.n, c.failed, c.over), 80, now)
		}
		if len(got) != c.lines {
			t.Errorf("%+v: %d lines, want %d:\n%s", c, len(got), c.lines, strings.Join(got, "\n"))
			continue
		}
		last := got[len(got)-1]
		if !strings.HasPrefix(got[0], c.first) {
			t.Errorf("%+v: first %q, want %q", c, got[0], c.first)
		}
		if c.summary != "" && !strings.HasSuffix(last, c.summary) {
			t.Errorf("%+v: last %q, want it to end %q", c, last, c.summary)
		}
		if c.summary == "" && strings.Contains(last, "+") {
			t.Errorf("%+v: unexpected summary %q", c, last)
		}
	}
}

// Slots fill in priority order: pending, failures (agents, then steps),
// errors, running agents; the last row counts what did not fit.
func TestSlotsPriority(t *testing.T) {
	now := time.Date(2026, 10, 26, 14, 0, 0, 0, time.UTC)
	r := &Renderer{}
	r.init()
	full := &Snapshot{
		Board: BoardFile{
			Pending: []Pending{{Text: "approve", Link: "https://x"}},
			Steps:   []Step{{Text: "hook", State: "failed", Note: "blocked"}, {Text: "ok", State: "done"}},
		},
		BoardErr: fmt.Errorf("board.json: bad"),
		Agents:   []Agent{{ID: "r", Name: "runner", Started: now.Add(-time.Minute)}, {ID: "f", Name: "broken", Failed: true}},
	}
	cases := []struct {
		name string
		edit func(s *Snapshot)
		want []string // row prefixes
		more string   // the last row's suffix, "" for none
	}{
		{"all categories", func(*Snapshot) {}, []string{"📥  approve", "🔴  broken", "✘   hook"}, " +2"},
		{"no pending", func(s *Snapshot) { s.Board.Pending = nil }, []string{"🔴  broken", "✘   hook", "⚠   board.json: bad"}, " +1"},
		{"only error and agent", func(s *Snapshot) { s.Board.Pending, s.Board.Steps, s.Agents = nil, nil, s.Agents[:1] },
			[]string{"⚠   board.json: bad", "🤖  runner", "📥  nothing on you"}, ""},
		{"only running", func(s *Snapshot) { s.Board.Pending, s.Board.Steps, s.BoardErr, s.Agents = nil, nil, nil, s.Agents[:1] },
			[]string{"🤖  runner", "📥  nothing on you", "🔴  no failures"}, ""},
		{"nothing", func(s *Snapshot) { *s = Snapshot{} },
			[]string{"📥  nothing on you", "🔴  no failures", "🤖  no agents running"}, ""},
	}
	for _, c := range cases {
		s := *full
		s.Board.Pending = append([]Pending(nil), full.Board.Pending...)
		c.edit(&s)
		got := r.slots(&s, 80, now)
		if len(got) != len(c.want) {
			t.Errorf("%s: %d rows, want %d:\n%s", c.name, len(got), len(c.want), strings.Join(got, "\n"))
			continue
		}
		for i, w := range c.want {
			if !strings.HasPrefix(got[i], w) {
				t.Errorf("%s: row %d = %q, want %q…", c.name, i, got[i], w)
			}
		}
		if last := got[len(got)-1]; c.more != "" && !strings.HasSuffix(last, c.more) || c.more == "" && strings.Contains(last, " +") {
			t.Errorf("%s: last row %q, want suffix %q", c.name, last, c.more)
		}
	}
	// pending rows: the text without its link, a `↗` on the right, no ┃ bar
	got := r.slots(full, 80, now)[0]
	if strings.Contains(got, "┃") || strings.Contains(got, "https://x") || !strings.HasPrefix(got, "📥  approve") || !strings.HasSuffix(got, "↗") {
		t.Errorf("pending row = %q", got)
	}
	// in color the text is a hyperlink to the link; a path becomes file://
	rc := &Renderer{Color: true}
	rc.init()
	link := &Snapshot{}
	link.Board.Pending = []Pending{{Text: "approve", Link: "https://x"}, {Text: "push", Link: "/home/m/repo"}}
	rows := rc.slots(link, 80, now)
	if !strings.Contains(rows[0], ansi.SetHyperlink("https://x")) || !strings.Contains(rows[1], ansi.SetHyperlink("file:///home/m/repo")) {
		t.Errorf("color pending rows lack hyperlinks: %q", rows[:2])
	}
	if got := ansi.Strip(rows[0]); strings.Contains(got, "https://x") || !strings.HasSuffix(got, "↗") {
		t.Errorf("color pending row shows its link: %q", got)
	}
}

// The fixed area is always six lines, the counts always all four.
func TestHeaderShape(t *testing.T) {
	now := time.Date(2026, 10, 26, 14, 0, 0, 0, time.UTC)
	for _, s := range []*Snapshot{
		{},
		{Status: Status{State: "done", Text: "ok"}},
		{Board: BoardFile{Pending: []Pending{{Text: "a"}, {Text: "b"}, {Text: "c"}, {Text: "d"}}}, Agents: []Agent{{Name: "x"}}},
	} {
		r := &Renderer{}
		top := strings.Split(r.Render(s, 60, now).Top, "\n")
		if len(top) != 6 || top[5] != strings.Repeat("─", 60) {
			t.Errorf("fixed area is %d lines, want 6 ending in a rule:\n%s", len(top), strings.Join(top, "\n"))
		}
		if !strings.HasPrefix(top[0], "⚪ (untitled)") && !strings.HasPrefix(top[0], "🟢 (untitled)") {
			t.Errorf("title line = %q", top[0])
		}
	}
	r := &Renderer{}
	top := strings.Split(r.Render(&Snapshot{}, 60, now).Top, "\n")
	if !strings.HasPrefix(top[1], "   ") || !strings.HasSuffix(top[1], "📥0 🔴0 🤖0 ⚠0") {
		t.Errorf("zero counts should stay: %q", top[1])
	}
}

// A zero count is drawn in the darker style; a non-zero one is not.
func TestZeroCountsDarker(t *testing.T) {
	withColor(t)
	r := &Renderer{Color: true}
	r.init()
	if r.st.off.Render("x") == r.st.dim.Render("x") {
		t.Error("the darker style should differ from dim")
	}
	got := r.counts(&Snapshot{Board: BoardFile{Pending: []Pending{{Text: "a"}}}}, lipgloss.Style{})
	if strings.Contains(got, r.st.off.Render("📥1")) || !strings.Contains(got, "📥1") {
		t.Errorf("non-zero count drawn darker: %q", got)
	}
	for _, z := range []string{"🔴0", "🤖0", "⚠0"} {
		if !strings.Contains(got, r.st.off.Render(z)) {
			t.Errorf("zero count %s not darker: %q", z, got)
		}
	}
}

// TestAgentsHidden: `a` has something to show exactly when an agent did not
// make it into the three header slots, however few agents there are.
func TestAgentsHidden(t *testing.T) {
	run := Agent{Name: "r"}
	bad := Agent{Name: "f", Failed: true}
	pend := []Pending{{Text: "p"}}
	cases := []struct {
		name    string
		pending []Pending
		agents  []Agent
		failed  int // failed steps
		want    bool
	}{
		{"three agents fit", nil, []Agent{run, run, run}, 0, false},
		{"four agents", nil, []Agent{run, run, run, run}, 0, true},
		{"pending pushes a running agent out", pend, []Agent{bad, run}, 1, true},
		{"failed agent still fits", pend, []Agent{bad}, 3, false},
		{"pending pushes a failed agent out", []Pending{{Text: "a"}, {Text: "b"}, {Text: "c"}}, []Agent{bad}, 0, true},
		{"no agents", pend, nil, 5, false},
	}
	for _, c := range cases {
		s := &Snapshot{Agents: c.agents}
		s.Board.Pending = c.pending
		for i := 0; i < c.failed; i++ {
			s.Board.Steps = append(s.Board.Steps, Step{State: "failed"})
		}
		if got := agentsHidden(s); got != c.want {
			t.Errorf("%s: agentsHidden = %v, want %v", c.name, got, c.want)
		}
	}
}
