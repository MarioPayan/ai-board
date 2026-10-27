package board

import (
	"fmt"
	"io"
	"math"
	"strings"
	"time"

	"sort"

	"github.com/charmbracelet/glamour"
	gstyles "github.com/charmbracelet/glamour/styles"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
)

// Renderer turns a Snapshot into text. With Color false it emits no escape
// sequences, so the output is stable for golden tests.
type Renderer struct {
	Color bool
	Dark  bool // glamour style when Color is true
	Options

	st     styles
	mdKey  string
	mdGlam *glamour.TermRenderer
	mdMemo map[string]string
}

// Options are the view toggles: keys in the live viewer, flags with --once.
type Options struct {
	Expand    bool // d, --expand: list every finished step instead of the collapsed summary
	AllChecks bool // c, --checks: list passing checks too, not only failing ones
	AllAgents bool // a, --agents: list every agent (in the body when one is not in the header slots)
	Legend    bool // g, --legend: show the fixed plan-marker legend line
}

// Colours are ANSI 16-colour indices so the terminal theme decides the shade.
type styles struct {
	title, dim, bold, rule, err, bar, barOver lipgloss.Style
	done, skipped, accent, active, failed     lipgloss.Style
	noteWaiting, noteFailed, noteExternal     lipgloss.Style
	off, doneBand, ok                         lipgloss.Style
}

// staleAfter is how old the newest file may be while the status is
// `working` before the header flags the board as stale.
const staleAfter = 10 * time.Minute

// collapseAbove is the number of finished steps shown one by one; above it
// they collapse into a single summary line unless Expand is set.
const collapseAbove = 3

func (r *Renderer) init() {
	if r.mdMemo != nil {
		return
	}
	lr := lipgloss.NewRenderer(io.Discard)
	if r.Color {
		lr.SetColorProfile(lipgloss.DefaultRenderer().ColorProfile())
	} else {
		lr.SetColorProfile(termenv.Ascii)
	}
	s := lr.NewStyle
	c := func(s string) lipgloss.Color { return lipgloss.Color(s) }
	r.st = styles{
		title:        s().Bold(true),
		dim:          s().Foreground(c("8")),
		bold:         s().Bold(true),
		rule:         s().Foreground(c("8")),
		err:          s().Foreground(c("1")),
		bar:          s().Foreground(c("4")),
		barOver:      s().Foreground(c("1")),
		done:         s().Foreground(c("7")),
		skipped:      s().Foreground(c("7")).Strikethrough(true),
		accent:       s().Foreground(c("6")).Bold(true),
		active:       s().Background(c("8")).Bold(true),
		failed:       s().Foreground(c("1")),
		noteWaiting:  s().Foreground(c("3")),
		noteFailed:   s().Foreground(c("1")),
		noteExternal: s().Foreground(c("5")),
		off:          s().Foreground(c("8")).Faint(true), // darker than dim: zero counts, empty slots, idle keys
		doneBand:     s().Background(c("2")).Foreground(c("0")).Bold(true),
		ok:           s().Foreground(c("2")),
	}
	r.mdMemo = map[string]string{}
}

// Frame is a rendered board: Top stays fixed, Body scrolls. Anchors are the
// Body line numbers that tab/shift+tab jump between: Agents (when in the
// body), Plan, each Details subsection and Log.
type Frame struct {
	Top     string
	Body    string
	Anchors []int
}

// block is a list of rendered lines.
type block struct {
	lines []string
}

func (b *block) add(line string) { b.lines = append(b.lines, line) }

func (b *block) addAll(lines []string) { b.lines = append(b.lines, lines...) }

func (b *block) cat(o block) { b.lines = append(b.lines, o.lines...) }

// Render renders the snapshot at the given width and clock.
func (r *Renderer) Render(s *Snapshot, width int, now time.Time) Frame {
	r.init()
	if width < 20 {
		width = 20
	}
	if s.Missing {
		return Frame{Top: r.st.dim.Render("⏳ waiting for " + s.Dir)}
	}
	// the fixed area always has the same six lines: title, status, three
	// slots and a rule
	top := r.header(s, width, now)
	top.addAll(r.slots(s, width, now))
	top.add(r.st.rule.Render(strings.Repeat("─", width)))

	var body block
	var anchors []int
	// top-level blocks are separated by one blank line; a subsection title
	// sits right on its body
	heading := func(icon, title string) {
		if len(body.lines) > 0 {
			body.add("")
		}
		anchors = append(anchors, len(body.lines))
		body.add(r.heading(icon, title, width))
	}
	if r.agentsInBody(s) {
		heading("🤖", "Agents")
		body.cat(r.agents(s, width, now))
	}
	if len(s.Board.Steps) > 0 {
		heading("📋", "Plan")
		body.cat(r.plan(s, width, now))
	}
	for i, sec := range s.Sections {
		if i == 0 {
			heading("🔎", "Details")
		} else {
			anchors = append(anchors, len(body.lines))
		}
		body.add(r.st.bold.Render(trunc(sec.Title, width)))
		body.cat(r.section(sec, width))
	}
	if log := r.logLines(s, width, 5); len(log.lines) > 0 {
		heading("📜", "Log")
		body.cat(log)
	}
	return Frame{Top: strings.Join(top.lines, "\n"), Body: strings.Join(body.lines, "\n"), Anchors: anchors}
}

// RenderLog renders the full log for the `l` view.
func (r *Renderer) RenderLog(s *Snapshot, width int) string {
	r.init()
	var b block
	b.add(r.heading("📜", "Log", width))
	b.cat(r.logLines(s, width, 0))
	return strings.Join(b.lines, "\n")
}

// Once renders a single uncolored frame as plain text. With o.Legend it ends
// with the fixed legend line.
func Once(s *Snapshot, width int, now time.Time, o Options) string {
	r := &Renderer{Options: o}
	f := r.Render(s, width, now)
	out := f.Top
	if f.Body != "" {
		out += "\n" + f.Body
	}
	out = strings.TrimRight(out, "\n")
	if o.Legend && !s.Missing {
		out += "\n\n" + r.LegendLine(width)
	}
	return out + "\n"
}

var statusEmoji = map[string]string{"done": "🟢", "working": "🔵", "waiting": "🟡", "blocked": "🔴"}

func finished(st Step) bool { return st.State == "done" || st.State == "skipped" }

func (r *Renderer) header(s *Snapshot, width int, now time.Time) block {
	title := s.Board.Title
	if title == "" {
		title = "(untitled)"
	}
	var right []string
	if el, ok := elapsed(s, now); ok {
		right = append(right, r.st.dim.Render("⏱ "+fmtDur(el)))
	}
	switch {
	case isStale(s, now):
		right = append(right, r.st.err.Render("⚠ stale "+ago(now.Sub(s.Updated))))
	case !s.Updated.IsZero():
		right = append(right, r.st.dim.Render("↻ "+ago(now.Sub(s.Updated))))
	}
	em := statusEmoji[s.Status.State]
	if em == "" {
		em = "⚪"
	}
	var b block
	b.add(row(em+" "+r.st.title.Render(title), strings.Join(right, r.st.dim.Render(" · ")), width))

	done := 0
	for _, st := range s.Board.Steps {
		if finished(st) {
			done++
		}
	}
	total := len(s.Board.Steps)
	frac := 0.0
	if total > 0 {
		frac = float64(done) / float64(total)
	}
	count := fmt.Sprintf(" %d/%d", done, total)
	if s.Status.State == "done" {
		// a green band: the work is over, whatever the step list says
		band := r.st.doneBand
		counts := r.counts(s, band)
		cw := ansi.StringWidth(counts)
		left := trunc("   "+s.Status.Text+"  "+meter(1, 10)+count, width-cw-2)
		gap := max(0, width-ansi.StringWidth(left)-cw)
		b.add(band.Render(left+strings.Repeat(" ", gap)) + counts)
		return b
	}
	l := "   " + r.st.bold.Render(s.Status.Text) + "  " + r.st.bar.Render(meter(frac, 10)) + count
	b.add(row(l, r.counts(s, lipgloss.Style{}), width))
	return b
}

// counts is `📥N 🔴N 🤖N 🟣N ⚠N`: items on you, failures (failed agents and
// steps), running agents, external waits (external steps) and errors (board.json and every parse error). A
// zero count is drawn darker, never left out, so the line keeps its shape.
// Every part inherits base.
func (r *Renderer) counts(s *Snapshot, base lipgloss.Style) string {
	failed, running, external := 0, 0, 0
	for _, a := range s.Agents {
		if a.Failed {
			failed++
		} else {
			running++
		}
	}
	for _, st := range s.Board.Steps {
		switch st.State {
		case "failed":
			failed++
		case "external":
			external++
		}
	}
	errs := len(s.AgentErrs) + len(s.LogErrs)
	if s.BoardErr != nil {
		errs++
	}
	for _, sec := range s.Sections {
		if sec.Err != nil {
			errs++
		}
	}
	var parts []string
	for _, c := range []struct {
		icon string
		n    int
	}{{"📥", len(s.Board.Pending)}, {"🔴", failed}, {"🤖", running}, {"🟣", external}, {"⚠", errs}} {
		st := base
		if c.n == 0 {
			st = r.st.off.Inherit(base)
		}
		parts = append(parts, st.Render(fmt.Sprintf("%s%d", c.icon, c.n)))
	}
	return strings.Join(parts, base.Render(" "))
}

// isStale reports whether a `working` board has not been written to for more
// than staleAfter.
func isStale(s *Snapshot, now time.Time) bool {
	return s.Status.State == "working" && !s.Updated.IsZero() && now.Sub(s.Updated) > staleAfter
}

// elapsed is the time since the first step's start; it stops at the last
// end once the board is done.
func elapsed(s *Snapshot, now time.Time) (time.Duration, bool) {
	var first time.Time
	for _, st := range s.Board.Steps {
		if t, err := time.Parse(time.RFC3339, st.Started); err == nil {
			first = t
			break
		}
	}
	if first.IsZero() {
		return 0, false
	}
	end := now
	if s.Status.State == "done" {
		var last time.Time
		for _, st := range s.Board.Steps {
			if t, err := time.Parse(time.RFC3339, st.Ended); err == nil && t.After(last) {
				last = t
			}
		}
		if !last.IsZero() {
			end = last
		}
	}
	return end.Sub(first), true
}

// agentsShown is the number of header slots: rows for what needs attention,
// whatever the board holds. The `a` key lists every agent in the body when
// some agent does not fit in them.
const agentsShown = 3

// slot is one header row: a table of icon | item | detail | right.
type slot struct {
	icon, item, detail, right string
	link                      string // the item is a terminal hyperlink to it (color only)
	iconSt, itemSt, detailSt  lipgloss.Style
}

// slots renders exactly agentsShown header rows, filled in priority order:
// pending items, failures (failed agents, then failed steps), errors
// (board.json, agents.jsonl), external waits (external steps), running agents. When there are more items the
// last row ends with a dim `+N`; when fewer, the free rows say, darker, which
// of pending, failures and agents is empty.
func (r *Renderer) slots(s *Snapshot, width int, now time.Time) []string {
	var items []slot
	for _, p := range s.Board.Pending {
		// the text gets the whole row: a link is a `↗` and, in color, a
		// hyperlink on the text, since a cut URL or path reads as nothing
		it := slot{icon: "📥", item: p.Text, link: p.Link, itemSt: r.st.bold}
		if p.Link != "" {
			it.right = r.st.dim.Render("↗")
		}
		items = append(items, it)
	}
	nPending := len(items)
	agents := sortAgents(s.Agents, now) // failed first
	nRunning := 0
	for _, a := range agents {
		if a.Failed {
			items = append(items, r.agentSlot(a, now))
		} else {
			nRunning++
		}
	}
	for _, st := range s.Board.Steps {
		if st.State == "failed" {
			items = append(items, slot{icon: "✘", item: st.Text, detail: st.Note, iconSt: r.st.err, itemSt: r.st.failed, detailSt: r.st.noteFailed})
		}
	}
	nFailed := len(items) - nPending
	var errs []string
	if s.BoardErr != nil {
		errs = append(errs, s.BoardErr.Error())
	}
	for _, e := range append(errs, s.AgentErrs...) {
		items = append(items, slot{icon: "⚠", item: e, iconSt: r.st.err, itemSt: r.st.err})
	}
	for _, st := range s.Board.Steps {
		if st.State != "external" {
			continue
		}
		it := slot{icon: "🟣", item: st.Text, detail: st.Note, link: st.Link, detailSt: r.st.noteExternal}
		var right []string
		if t, err := time.Parse(time.RFC3339, st.Started); err == nil {
			right = append(right, fmtDur(now.Sub(t)))
		}
		if st.Link != "" {
			right = append(right, "↗")
		}
		it.right = r.st.dim.Render(strings.Join(right, " "))
		items = append(items, it)
	}
	for _, a := range agents[len(agents)-nRunning:] {
		items = append(items, r.agentSlot(a, now))
	}
	if n := len(items) - agentsShown; n > 0 {
		items = items[:agentsShown]
		last := &items[agentsShown-1]
		last.right = strings.TrimSpace(last.right + " " + r.st.dim.Render(fmt.Sprintf("+%d", n)))
	}

	// column widths: icons are two cells, the right column is right-aligned
	// at the edge, the item column is as wide as its widest entry and the
	// detail column takes what is left
	var iw, dw, rw int
	for _, it := range items {
		iw = max(iw, ansi.StringWidth(it.item))
		dw = max(dw, ansi.StringWidth(it.detail))
		rw = max(rw, ansi.StringWidth(it.right))
	}
	rw = min(rw, width/2)
	room := width - 4 // icon and gap
	if rw > 0 {
		room -= rw + 2
	}
	if dw > 0 {
		iw = min(iw, max(room/2, room-2-dw))
	}
	iw = min(iw, room)
	var out []string
	for _, it := range items {
		item := it.itemSt.Render(pad(it.item, iw, false))
		if it.link != "" && r.Color {
			item = ansi.SetHyperlink(hyperlinkURI(it.link)) + item + ansi.ResetHyperlink()
		}
		l := it.iconSt.Render(pad(it.icon, 2, false)) + "  " + item
		if d := room - iw - 2; it.detail != "" && d > 0 {
			l += "  " + it.detailSt.Render(trunc(it.detail, d))
		}
		if it.right != "" {
			right := ansi.Truncate(it.right, rw, "…")
			l += strings.Repeat(" ", max(0, width-ansi.StringWidth(l)-ansi.StringWidth(right))) + right
		}
		out = append(out, strings.TrimRight(l, " "))
	}
	for _, e := range []struct {
		icon, text string
		empty      bool
	}{{"📥", "nothing on you", nPending == 0}, {"🔴", "no failures", nFailed == 0}, {"🤖", "no agents running", nRunning == 0}} {
		if e.empty && len(out) < agentsShown {
			out = append(out, r.st.off.Render(trunc(e.icon+"  "+e.text, width)))
		}
	}
	return out
}

// hyperlinkURI makes a pending link openable from a terminal hyperlink: an
// absolute path becomes a file:// URI, anything else is used as given.
func hyperlinkURI(link string) string {
	if strings.HasPrefix(link, "/") {
		return "file://" + link
	}
	return link
}

// agentSlot is a header row for a failed or running agent.
func (r *Renderer) agentSlot(a Agent, now time.Time) slot {
	if a.Failed {
		right := r.st.err.Render("failed")
		if !a.Stopped.IsZero() {
			right += " " + a.Stopped.Format("15:04")
		}
		return slot{icon: "🔴", item: a.Name, right: right}
	}
	return slot{icon: "🤖", item: a.Name, right: r.agentTime(a, now)}
}

func agentElapsed(a Agent, now time.Time) time.Duration {
	if a.Started.IsZero() {
		return 0
	}
	return now.Sub(a.Started)
}

// agentOver reports whether a running agent has passed its expected time.
func agentOver(a Agent, now time.Time) bool {
	return !a.Failed && a.ExpectMin > 0 && agentElapsed(a, now) > time.Duration(a.ExpectMin*float64(time.Minute))
}

// sortAgents returns the agents failed first, then over their expected
// time, then by elapsed time, longest first; ties keep file order.
func sortAgents(in []Agent, now time.Time) []Agent {
	out := append([]Agent(nil), in...)
	rank := func(a Agent) int {
		switch {
		case a.Failed:
			return 0
		case agentOver(a, now):
			return 1
		}
		return 2
	}
	sort.SliceStable(out, func(i, j int) bool {
		if ri, rj := rank(out[i]), rank(out[j]); ri != rj {
			return ri < rj
		}
		return agentElapsed(out[i], now) > agentElapsed(out[j], now)
	})
	return out
}

// agentsInBody reports whether `a` lists every agent in the scrolling body:
// only when some agent did not make it into the header slots.
func (r *Renderer) agentsInBody(s *Snapshot) bool {
	return r.AllAgents && agentsHidden(s)
}

// agentsHidden reports whether an agent is left out of the header slots, by
// the same order slots fills them: pending, failed agents, failed steps,
// errors, external steps, running agents.
func agentsHidden(s *Snapshot) bool {
	var failed, running, rest int
	for _, a := range s.Agents {
		if a.Failed {
			failed++
		} else {
			running++
		}
	}
	for _, st := range s.Board.Steps {
		if st.State == "failed" || st.State == "external" {
			rest++
		}
	}
	rest += len(s.AgentErrs)
	if s.BoardErr != nil {
		rest++
	}
	pending := len(s.Board.Pending)
	if running > 0 {
		return pending+failed+rest+running > agentsShown
	}
	return pending+failed > agentsShown
}

// agents renders every agent, sorted, one per line, then any agents.jsonl
// parse errors.
func (r *Renderer) agents(s *Snapshot, width int, now time.Time) block {
	var b block
	for _, a := range sortAgents(s.Agents, now) {
		sl := r.agentSlot(a, now)
		b.add(row(sl.icon+" "+sl.item, sl.right, width))
	}
	for _, e := range s.AgentErrs {
		b.add(r.errLine(e, width))
	}
	return b
}

// agentTime is a running agent's elapsed time or, with an expected time, a
// bar of elapsed against expected, marked `over` once exceeded.
func (r *Renderer) agentTime(a Agent, now time.Time) string {
	el := agentElapsed(a, now)
	if a.ExpectMin <= 0 {
		return fmtDur(el)
	}
	exp := time.Duration(a.ExpectMin * float64(time.Minute))
	frac := float64(el) / float64(exp)
	bs := r.st.bar
	if frac > 1 {
		bs = r.st.barOver
	}
	right := bs.Render(meter(frac, 10)) + " " + fmtDur(el) + "/" + fmtDur(exp)
	if frac > 1 {
		right += " " + r.st.err.Render("over")
	}
	return right
}

// Markers are two cells wide so step text lines up after emoji markers.
var stepMarker = map[string]string{
	"done": "✔ ", "active": "▶ ", "waiting": "🟡", "external": "🟣",
	"failed": "✘ ", "todo": "○ ", "skipped": "✔ ",
}

// stepDur is ended - started of a finished step, or "" when either
// timestamp is missing or unparsable.
func stepDur(st Step) string {
	a, err1 := time.Parse(time.RFC3339, st.Started)
	b, err2 := time.Parse(time.RFC3339, st.Ended)
	if err1 != nil || err2 != nil {
		return ""
	}
	return fmtDur(b.Sub(a))
}

func (r *Renderer) plan(s *Snapshot, width int, now time.Time) block {
	steps := s.Board.Steps
	nDone, last := 0, -1
	var lastEnd time.Time
	for i, st := range steps {
		if !finished(st) {
			continue
		}
		nDone++
		t, err := time.Parse(time.RFC3339, st.Ended)
		switch {
		case err == nil && (last < 0 || lastEnd.IsZero() || !t.Before(lastEnd)):
			last, lastEnd = i, t
		case err != nil && lastEnd.IsZero():
			last = i // no timestamps: the latest in list order
		}
	}
	collapse := nDone > collapseAbove && !r.Expand

	var b block
	summarised := false
	for _, st := range steps {
		if collapse && finished(st) {
			if !summarised {
				summarised = true
				text := fmt.Sprintf("%d · last: %s", nDone, steps[last].Text)
				b.add(r.stepLine("✔ ", text, "", r.st.done, r.st.dim, stepDur(steps[last]), r.st.dim, lipgloss.Style{}, width))
			}
			continue
		}
		b.add(r.step(st, width, now))
	}
	return b
}

func (r *Renderer) step(st Step, width int, now time.Time) string {
	m := stepMarker[st.State]
	if m == "" {
		m = "? "
	}
	textSt, noteSt, rightSt := lipgloss.NewStyle(), r.st.dim, r.st.dim
	var base lipgloss.Style
	var right string
	switch st.State {
	case "done":
		textSt, right = r.st.done, stepDur(st)
	case "skipped":
		textSt, right = r.st.skipped, stepDur(st)
	case "active":
		if t, err := time.Parse(time.RFC3339, st.Started); err == nil {
			right = "⏱ " + fmtDur(now.Sub(t))
		}
		// the row background would hide dim text: keep the default foreground
		base, noteSt, rightSt = r.st.active, lipgloss.NewStyle(), lipgloss.NewStyle()
		return r.stepLine(r.st.accent.Inherit(base).Render(m), st.Text, st.Note, textSt, noteSt, right, rightSt, base, width)
	case "waiting":
		noteSt = r.st.noteWaiting
	case "failed":
		textSt, noteSt = r.st.failed, r.st.noteFailed
	case "external":
		noteSt = r.st.noteExternal
	}
	return r.stepLine(m, st.Text, st.Note, textSt, noteSt, right, rightSt, base, width)
}

const noteSep = " — "

// stepLine lays out `marker text — note … right` on one row of width cells.
// Every piece inherits base, so a background covers the whole row.
func (r *Renderer) stepLine(marker, text, note string, textSt, noteSt lipgloss.Style, right string, rightSt, base lipgloss.Style, width int) string {
	rw := ansi.StringWidth(right)
	if rw > width/2 {
		right = ansi.Truncate(right, width/2, "…")
		rw = ansi.StringWidth(right)
	}
	room := width - ansi.StringWidth(marker) - 1
	if right != "" {
		room -= rw + 2
	}
	text, note = fitStep(text, note, room)
	left := marker + base.Render(" ") + textSt.Inherit(base).Render(text)
	if note != "" {
		left += noteSt.Inherit(base).Render(noteSep + note)
	}
	if right == "" && base.GetBackground() == (lipgloss.NoColor{}) {
		return left
	}
	gap := width - ansi.StringWidth(left) - rw
	return left + base.Render(strings.Repeat(" ", max(0, gap))) + rightSt.Inherit(base).Render(right)
}

// fitStep truncates a step's text and note to room cells: the note shrinks
// first (down to minNote cells), then the text (down to minText cells); if
// both still do not fit, the note is dropped.
func fitStep(text, note string, room int) (string, string) {
	const minNote, minText = 6, 20
	sep := ansi.StringWidth(noteSep)
	tw, nw := ansi.StringWidth(text), ansi.StringWidth(note)
	if note == "" {
		return trunc(text, room), ""
	}
	if tw+sep+nw <= room {
		return text, note
	}
	if n := room - tw - sep; n >= minNote {
		return text, trunc(note, n)
	}
	nk := min(nw, minNote)
	if t := room - sep - nk; t >= min(tw, minText) {
		return trunc(text, t), trunc(note, nk)
	}
	return trunc(text, room), ""
}

var logMarker = map[string]string{"commit": "◆", "note": "•", "link": "↗"}

func (r *Renderer) logLines(s *Snapshot, width, max int) block {
	var b block
	for i, e := range s.Log {
		if max > 0 && i >= max {
			break
		}
		m := logMarker[e.Kind]
		if m == "" {
			m = "·"
		}
		ts := "     "
		if !e.t.IsZero() {
			ts = e.t.Format("15:04")
		}
		b.add(trunc(r.st.dim.Render(ts)+" "+m+" "+e.Text, width))
	}
	for _, e := range s.LogErrs {
		b.add(r.errLine(e, width))
	}
	return b
}

func (r *Renderer) section(sec Section, width int) block {
	var b block
	if sec.Err != nil {
		b.add(r.errLine(sec.Name+": "+sec.Err.Error(), width))
		return b
	}
	switch sec.Kind {
	case "md":
		b.addAll(r.markdown(sec, width))
	case "chart":
		b.addAll(r.chart(sec.Chart, width))
	case "table":
		b.addAll(r.table(sec.Table, width))
	case "checks":
		return r.checks(sec.Checks, width)
	}
	return b
}

// inlineCodeColor is ANSI cyan for `inline code` in the live styles.
var inlineCodeColor = "6"

func (r *Renderer) markdown(sec Section, width int) []string {
	key := fmt.Sprintf("%d", width)
	if r.mdGlam == nil || r.mdKey != key {
		style := gstyles.NoTTYStyleConfig
		if r.Color {
			style = gstyles.LightStyleConfig
			if r.Dark {
				style = gstyles.DarkStyleConfig
			}
		}
		// the standard styles pad inline code with a space on each side
		style.Code.StylePrimitive.Prefix, style.Code.StylePrimitive.Suffix = "", ""
		if r.Color {
			// and paint it red (203) on grey, which reads like an error
			style.Code.StylePrimitive.Color = &inlineCodeColor
			style.Code.StylePrimitive.BackgroundColor = nil
		}
		g, err := glamour.NewTermRenderer(glamour.WithStyles(style), glamour.WithWordWrap(width-2))
		if err != nil {
			return []string{r.errLine(err.Error(), width)}
		}
		r.mdGlam, r.mdKey = g, key
		r.mdMemo = map[string]string{}
	}
	memo := sec.Name + "\x00" + string(sec.Body)
	out, ok := r.mdMemo[memo]
	if !ok {
		var err error
		out, err = r.mdGlam.Render(string(sec.Body))
		if err != nil {
			return []string{r.errLine(sec.Name+": "+err.Error(), width)}
		}
		out = strings.Trim(out, "\n")
		r.mdMemo[memo] = out
	}
	var lines []string
	for _, l := range strings.Split(out, "\n") {
		if !r.Color {
			l = strings.TrimRight(l, " ")
		}
		lines = append(lines, l)
	}
	// glamour pads every block with blank lines; trim the edges
	for len(lines) > 0 && strings.TrimSpace(ansi.Strip(lines[0])) == "" {
		lines = lines[1:]
	}
	for len(lines) > 0 && strings.TrimSpace(ansi.Strip(lines[len(lines)-1])) == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

// heading is a section title in bold followed by a dim rule to the edge.
func (r *Renderer) heading(icon, title string, width int) string {
	head := icon + " " + title
	n := width - ansi.StringWidth(head) - 1
	if n < 1 {
		return r.st.bold.Render(trunc(head, width))
	}
	return r.st.bold.Render(head) + " " + r.st.rule.Render(strings.Repeat("─", n))
}

func (r *Renderer) errLine(msg string, width int) string {
	return r.st.err.Render(trunc("⚠ "+msg, width))
}

// row puts left and right on one line of the given width, truncating left.
func row(left, right string, width int) string {
	rw := ansi.StringWidth(right)
	if right == "" {
		return trunc(left, width)
	}
	if rw > width/2 {
		right = ansi.Truncate(right, width/2, "…")
		rw = ansi.StringWidth(right)
	}
	left = trunc(left, width-rw-2)
	gap := width - ansi.StringWidth(left) - rw
	return left + strings.Repeat(" ", gap) + right
}

func trunc(s string, width int) string {
	if width < 1 {
		return ""
	}
	return ansi.Truncate(s, width, "…")
}

func fmtDur(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	default:
		return fmt.Sprintf("%dh%02dm", int(d.Hours()), int(d.Minutes())%60)
	}
}

func ago(d time.Duration) string {
	if d < 5*time.Second {
		return "just now"
	}
	return fmtDur(d) + " ago"
}

func fmtNum(v float64) string {
	if v == math.Trunc(v) && math.Abs(v) < 1e15 {
		return fmt.Sprintf("%d", int64(v))
	}
	return strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.2f", v), "0"), ".")
}

// checks renders a *.checks.json section: one green `✔ N/N checks` line when
// all pass, otherwise a red `✘ F/N checks failed` line followed by the
// failing checks. With AllChecks, every check is listed.
func (r *Renderer) checks(c *Checks, width int) block {
	n, failed := len(c.Checks), 0
	for _, ch := range c.Checks {
		if !ch.OK {
			failed++
		}
	}
	var b block
	if n == 0 {
		b.add(r.st.dim.Render("no checks"))
		return b
	}
	if failed == 0 {
		b.add(r.st.ok.Render(trunc(fmt.Sprintf("✔ %d/%d checks", n, n), width)))
	} else {
		b.add(r.st.err.Render(trunc(fmt.Sprintf("✘ %d/%d checks failed", failed, n), width)))
	}
	for _, ch := range c.Checks {
		if ch.OK && !r.AllChecks {
			continue
		}
		marker, st := r.st.ok.Render("✔"), lipgloss.NewStyle()
		if !ch.OK {
			marker, st = r.st.err.Render("✘"), r.st.failed
		}
		b.add(r.stepLine("  "+marker, ch.Name, ch.Detail, st, r.st.dim, "", r.st.dim, lipgloss.Style{}, width))
	}
	return b
}
