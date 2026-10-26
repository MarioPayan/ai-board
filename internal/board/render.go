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
	AllAgents bool // a, --agents: list every agent (in the body when more than agentsShown)
}

// Colours are ANSI 16-colour indices so the terminal theme decides the shade.
type styles struct {
	title, dim, bold, rule, err, bar, barOver lipgloss.Style
	done, skipped, accent, active, failed     lipgloss.Style
	noteWaiting, noteFailed, noteExternal     lipgloss.Style
	pendBar, doneBand, ok                     lipgloss.Style
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
		pendBar:      s().Foreground(c("3")).Bold(true),
		doneBand:     s().Background(c("2")).Foreground(c("0")).Bold(true),
		ok:           s().Foreground(c("2")),
	}
	r.mdMemo = map[string]string{}
}

// Frame is a rendered board: Top stays fixed, Body scrolls. Anchors are the
// Body line numbers that tab/shift+tab jump between: Plan, each Details
// subsection and Log.
type Frame struct {
	Top     string
	Body    string
	Anchors []int
}

// Render renders the snapshot at the given width and clock.
func (r *Renderer) Render(s *Snapshot, width int, now time.Time) Frame {
	r.init()
	if width < 20 {
		width = 20
	}
	if s.Missing {
		return Frame{Top: r.st.dim.Render("⏳ waiting for " + s.Dir)}
	}
	var top []string
	top = append(top, r.header(s, width, now)...)
	top = append(top, r.pending(s, width)...)
	if s.BoardErr != nil {
		top = append(top, r.errLine(s.BoardErr.Error(), width))
	}
	if !r.agentsInBody(s) {
		top = append(top, r.agents(s, width, now, false)...)
	}

	var body []string
	var anchors []int
	// top-level blocks are separated by one blank line; a subsection title
	// sits right on its body
	gap := func() {
		if len(body) > 0 {
			body = append(body, "")
		}
	}
	if r.agentsInBody(s) {
		anchors = append(anchors, len(body))
		body = append(body, r.heading("🤖", "Agents", width))
		body = append(body, r.agents(s, width, now, true)...)
	}
	if len(s.Board.Steps) > 0 {
		gap()
		anchors = append(anchors, len(body))
		body = append(body, r.heading("📋", "Plan", width))
		body = append(body, r.plan(s, width, now)...)
	}
	for i, sec := range s.Sections {
		if i == 0 {
			gap()
			anchors = append(anchors, len(body))
			body = append(body, r.heading("🔎", "Details", width))
		} else {
			anchors = append(anchors, len(body))
		}
		body = append(body, r.st.bold.Render(trunc(sec.Title, width)))
		body = append(body, r.section(sec, width)...)
	}
	if log := r.logLines(s, width, 5); len(log) > 0 {
		gap()
		anchors = append(anchors, len(body))
		body = append(body, r.heading("📜", "Log", width))
		body = append(body, log...)
	}
	if len(body) > 0 {
		top = append(top, "") // a blank line between the fixed area and the body
	}
	return Frame{Top: strings.Join(top, "\n"), Body: strings.Join(body, "\n"), Anchors: anchors}
}

// RenderLog renders the full log for the `l` view.
func (r *Renderer) RenderLog(s *Snapshot, width int) string {
	r.init()
	return strings.Join(append([]string{r.heading("📜", "Log", width)}, r.logLines(s, width, 0)...), "\n")
}

// Once renders a single uncolored frame as plain text. Finished steps are
// collapsed unless expand is set.
func Once(s *Snapshot, width int, now time.Time, o Options) string {
	r := &Renderer{Options: o}
	f := r.Render(s, width, now)
	out := f.Top
	if f.Body != "" {
		out += "\n" + f.Body
	}
	return strings.TrimRight(out, "\n") + "\n"
}

var statusEmoji = map[string]string{"done": "🟢", "working": "🔵", "waiting": "🟡", "blocked": "🔴"}

func finished(st Step) bool { return st.State == "done" || st.State == "skipped" }

func (r *Renderer) header(s *Snapshot, width int, now time.Time) []string {
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
	lines := []string{row(r.st.title.Render(title), strings.Join(right, r.st.dim.Render(" · ")), width)}

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
		l := trunc("🟢 "+s.Status.Text+"  "+meter(1, 10)+count, width)
		if r.Color {
			l += strings.Repeat(" ", max(0, width-ansi.StringWidth(l)))
		}
		return append(lines, r.st.doneBand.Render(l))
	}
	em := statusEmoji[s.Status.State]
	if em == "" {
		em = "⚪"
	}
	l := em + " " + r.st.bold.Render(s.Status.Text) + "  " + r.st.bar.Render(meter(frac, 10)) + count
	return append(lines, trunc(l, width))
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

func (r *Renderer) pending(s *Snapshot, width int) []string {
	var lines []string
	for _, p := range s.Board.Pending {
		l := r.st.pendBar.Render("┃") + " 📥 " + r.st.bold.Render(p.Text)
		if p.Link != "" {
			l += "  " + r.st.dim.Render(p.Link)
		}
		lines = append(lines, trunc(l, width))
	}
	return lines
}

// agentsShown is how many agents the fixed area lists before the rest
// collapse into one `🤖 +N more` line.
const agentsShown = 3

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

// agentsInBody reports whether the agent list moves from the fixed area
// into the scrolling body: only when expanded and it would not fit.
func (r *Renderer) agentsInBody(s *Snapshot) bool {
	return r.AllAgents && len(s.Agents) > agentsShown
}

// agents renders the sorted agent list: every agent when all is set,
// otherwise the first agentsShown and a `🤖 +N more (M over)` line for the
// rest, M counting the hidden ones over their expected time.
func (r *Renderer) agents(s *Snapshot, width int, now time.Time, all bool) []string {
	agents := sortAgents(s.Agents, now)
	var hidden []Agent
	if !all && len(agents) > agentsShown {
		agents, hidden = agents[:agentsShown], agents[agentsShown:]
	}
	var lines []string
	for _, a := range agents {
		lines = append(lines, r.agentLine(a, width, now))
	}
	if len(hidden) > 0 {
		over := 0
		for _, a := range hidden {
			if agentOver(a, now) {
				over++
			}
		}
		l := fmt.Sprintf("🤖 +%d more", len(hidden))
		if over > 0 {
			l += fmt.Sprintf(" (%d over)", over)
		}
		lines = append(lines, r.st.dim.Render(trunc(l, width)))
	}
	for _, e := range s.AgentErrs {
		lines = append(lines, r.errLine(e, width))
	}
	return lines
}

func (r *Renderer) agentLine(a Agent, width int, now time.Time) string {
	var marker, right string
	if a.Failed {
		marker = "🔴"
		right = r.st.err.Render("failed")
		if !a.Stopped.IsZero() {
			right += " " + a.Stopped.Format("15:04")
		}
	} else {
		marker = "🤖"
		el := agentElapsed(a, now)
		right = fmtDur(el)
		if a.ExpectMin > 0 {
			exp := time.Duration(a.ExpectMin * float64(time.Minute))
			frac := float64(el) / float64(exp)
			bs := r.st.bar
			if frac > 1 {
				bs = r.st.barOver
			}
			right = bs.Render(meter(frac, 10)) + " " + fmtDur(el) + "/" + fmtDur(exp)
			if frac > 1 {
				right += " " + r.st.err.Render("over")
			}
		}
	}
	return row(marker+" "+a.Name, right, width)
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

func (r *Renderer) plan(s *Snapshot, width int, now time.Time) []string {
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

	var lines []string
	summarised := false
	for _, st := range steps {
		if collapse && finished(st) {
			if !summarised {
				summarised = true
				text := fmt.Sprintf("%d · last: %s", nDone, steps[last].Text)
				lines = append(lines, r.stepLine("✔ ", text, "", r.st.done, r.st.dim, stepDur(steps[last]), r.st.dim, lipgloss.Style{}, width))
			}
			continue
		}
		lines = append(lines, r.step(st, width, now))
	}
	return lines
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

func (r *Renderer) logLines(s *Snapshot, width, max int) []string {
	var lines []string
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
		lines = append(lines, trunc(r.st.dim.Render(ts)+" "+m+" "+e.Text, width))
	}
	for _, e := range s.LogErrs {
		lines = append(lines, r.errLine(e, width))
	}
	return lines
}

func (r *Renderer) section(sec Section, width int) []string {
	if sec.Err != nil {
		return []string{r.errLine(sec.Name+": "+sec.Err.Error(), width)}
	}
	switch sec.Kind {
	case "md":
		return r.markdown(sec, width)
	case "chart":
		return r.chart(sec.Chart, width)
	case "table":
		return r.table(sec.Table, width)
	case "checks":
		return r.checks(sec.Checks, width)
	}
	return nil
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
func (r *Renderer) checks(c *Checks, width int) []string {
	n, failed := len(c.Checks), 0
	for _, ch := range c.Checks {
		if !ch.OK {
			failed++
		}
	}
	if n == 0 {
		return []string{r.st.dim.Render("no checks")}
	}
	var lines []string
	if failed == 0 {
		lines = append(lines, r.st.ok.Render(trunc(fmt.Sprintf("✔ %d/%d checks", n, n), width)))
	} else {
		lines = append(lines, r.st.err.Render(trunc(fmt.Sprintf("✘ %d/%d checks failed", failed, n), width)))
	}
	for _, ch := range c.Checks {
		if ch.OK && !r.AllChecks {
			continue
		}
		marker, st := r.st.ok.Render("✔"), lipgloss.NewStyle()
		if !ch.OK {
			marker, st = r.st.err.Render("✘"), r.st.failed
		}
		lines = append(lines, r.stepLine("  "+marker, ch.Name, ch.Detail, st, r.st.dim, "", r.st.dim, lipgloss.Style{}, width))
	}
	return lines
}
