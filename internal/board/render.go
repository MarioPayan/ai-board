package board

import (
	"fmt"
	"io"
	"math"
	"strings"
	"time"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
)

// Renderer turns a Snapshot into text. With Color false it emits no escape
// sequences, so the output is stable for golden tests.
type Renderer struct {
	Color bool
	Dark  bool // glamour style when Color is true

	st     styles
	mdKey  string
	mdGlam *glamour.TermRenderer
	mdMemo map[string]string
}

type styles struct {
	title, dim, bold, rule, err, bar, barOver, strike lipgloss.Style
}

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
	r.st = styles{
		title:   s().Bold(true),
		dim:     s().Foreground(lipgloss.Color("8")),
		bold:    s().Bold(true),
		rule:    s().Foreground(lipgloss.Color("6")),
		err:     s().Foreground(lipgloss.Color("1")),
		bar:     s().Foreground(lipgloss.Color("4")),
		barOver: s().Foreground(lipgloss.Color("1")),
		strike:  s().Foreground(lipgloss.Color("8")).Strikethrough(true),
	}
	r.mdMemo = map[string]string{}
}

// Frame is a rendered board: Top stays fixed, Body scrolls. Anchors are the
// Body line numbers where each block (Plan, Pending, sections, Log) starts.
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
		return Frame{Top: r.st.dim.Render("waiting for " + s.Dir)}
	}
	var top []string
	top = append(top, r.header(s, width, now)...)
	top = append(top, r.agents(s, width, now)...)

	var body []string
	var anchors []int
	block := func(title string, lines []string) {
		anchors = append(anchors, len(body))
		body = append(body, r.rule(title, width))
		body = append(body, lines...)
	}
	block("Plan", r.plan(s, width, now))
	block("Pending", r.pending(s, width))
	for _, sec := range s.Sections {
		block(sec.Title, r.section(sec, width))
	}
	block("Log", r.logLines(s, width, 5))
	return Frame{Top: strings.Join(top, "\n"), Body: strings.Join(body, "\n"), Anchors: anchors}
}

// RenderLog renders the full log for the `l` view.
func (r *Renderer) RenderLog(s *Snapshot, width int) string {
	r.init()
	return strings.Join(append([]string{r.rule("Log", width)}, r.logLines(s, width, 0)...), "\n")
}

// Once renders a single uncolored frame as plain text.
func Once(s *Snapshot, width int, now time.Time) string {
	r := &Renderer{}
	f := r.Render(s, width, now)
	out := f.Top
	if f.Body != "" {
		out += "\n" + f.Body
	}
	return strings.TrimRight(out, "\n") + "\n"
}

var statusEmoji = map[string]string{"done": "🟢", "working": "🔵", "waiting": "🟡", "blocked": "🔴"}

func (r *Renderer) header(s *Snapshot, width int, now time.Time) []string {
	title := s.Board.Title
	if title == "" {
		title = "(untitled)"
	}
	lines := []string{trunc(r.st.title.Render(title), width)}

	done := 0
	for _, st := range s.Board.Steps {
		if st.State == "done" || st.State == "skipped" {
			done++
		}
	}
	total := len(s.Board.Steps)
	frac := 0.0
	if total > 0 {
		frac = float64(done) / float64(total)
	}
	em := statusEmoji[s.Status.State]
	if em == "" {
		em = "⚪"
	}
	parts := []string{em + " " + r.st.bold.Render(s.Status.Text),
		r.st.bar.Render(meter(frac, 10)) + fmt.Sprintf(" %d/%d", done, total)}
	if el, ok := elapsed(s, now); ok {
		parts = append(parts, fmtDur(el)+" elapsed")
	}
	if !s.Updated.IsZero() {
		parts = append(parts, r.st.dim.Render("updated "+ago(now.Sub(s.Updated))))
	}
	lines = append(lines, trunc(strings.Join(parts, "  "), width))
	if s.BoardErr != nil {
		lines = append(lines, r.errLine(s.BoardErr.Error(), width))
	}
	return lines
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

func (r *Renderer) agents(s *Snapshot, width int, now time.Time) []string {
	var lines []string
	for _, a := range s.Agents {
		var marker, right string
		if a.Failed {
			marker = "🔴"
			right = r.st.err.Render("failed")
			if !a.Stopped.IsZero() {
				right += " " + a.Stopped.Format("15:04")
			}
		} else {
			marker = "🔵"
			el := now.Sub(a.Started)
			if a.Started.IsZero() {
				el = 0
			}
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
		lines = append(lines, row(marker+" "+a.Name, right, width))
	}
	for _, e := range s.AgentErrs {
		lines = append(lines, r.errLine(e, width))
	}
	return lines
}

var stepMarker = map[string]string{
	"done": "✔ ", "active": "● ", "waiting": "🟡", "external": "🟣",
	"failed": "✘ ", "todo": "○ ", "skipped": "✔ ",
}

func (r *Renderer) plan(s *Snapshot, width int, now time.Time) []string {
	if len(s.Board.Steps) == 0 {
		return []string{r.st.dim.Render("no steps")}
	}
	var lines []string
	for _, st := range s.Board.Steps {
		m := stepMarker[st.State]
		if m == "" {
			m = "? "
		}
		text := st.Text
		var right string
		switch st.State {
		case "done":
			if t, err := time.Parse(time.RFC3339, st.Ended); err == nil {
				right = t.Format("15:04")
			}
		case "active":
			if t, err := time.Parse(time.RFC3339, st.Started); err == nil {
				right = fmtDur(now.Sub(t))
			}
		}
		if st.Note != "" {
			if right != "" {
				right = st.Note + " · " + right
			} else {
				right = st.Note
			}
		}
		left := m + " " + text
		switch st.State {
		case "done":
			lines = append(lines, r.st.dim.Render(row(left, right, width)))
		case "skipped":
			right = strings.TrimSuffix("skipped: "+st.Note, ": ")
			lines = append(lines, r.st.strike.Render(row(left, right, width)))
		case "active":
			lines = append(lines, r.st.bold.Render(row(left, right, width)))
		case "failed":
			lines = append(lines, r.st.err.Render(row(left, right, width)))
		default:
			lines = append(lines, row(left, right, width))
		}
	}
	return lines
}

func (r *Renderer) pending(s *Snapshot, width int) []string {
	if len(s.Board.Pending) == 0 {
		return []string{r.st.dim.Render("nothing pending")}
	}
	var lines []string
	for _, p := range s.Board.Pending {
		l := "→ " + p.Text
		if p.Link != "" {
			l += "  " + r.st.dim.Render(p.Link)
		}
		lines = append(lines, trunc(l, width))
	}
	return lines
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
	if len(lines) == 0 {
		lines = append(lines, r.st.dim.Render("no entries"))
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
	}
	return nil
}

func (r *Renderer) markdown(sec Section, width int) []string {
	key := fmt.Sprintf("%d", width)
	if r.mdGlam == nil || r.mdKey != key {
		style := "notty"
		if r.Color {
			style = "light"
			if r.Dark {
				style = "dark"
			}
		}
		g, err := glamour.NewTermRenderer(glamour.WithStandardStyle(style), glamour.WithWordWrap(width-2))
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

func (r *Renderer) rule(title string, width int) string {
	head := "── " + title + " "
	n := width - ansi.StringWidth(head)
	if n < 0 {
		return r.st.rule.Render(trunc(head, width))
	}
	return r.st.rule.Render(head + strings.Repeat("─", n))
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
