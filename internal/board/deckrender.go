package board

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// Deck view targets: what a body line does when clicked. opt is an option
// index or one of the tgt* values; item is an index into the open items,
// len(open items) being the general note.
const (
	tgtHeader  = -1 // nothing: the item's own lines
	tgtNote    = -2 // edit its note
	tgtURL     = -3 // open its url
	tgtOther   = -4 // pick Other and edit its text
	tgtSuggest = -5 // ask Claude for another option
	tgtGo      = -6 // a review row: go to that decision
)

// deckTarget is a click target. With note set, opt is an option index and
// the click edits the note on that option.
type deckTarget struct {
	item, opt int
	note      bool
}

// deckBody is one rendered page of the deck view (a decision, or the
// review): its lines, the click target of each line that has one, and, on
// option lines that show `+ note`, the column where it starts.
type deckBody struct {
	lines   []string
	targets map[int]deckTarget
	noteX   map[int]int
}

// deckFocus is what the deck view shows: the page (an index into the open
// items, len(open items) being the review), the focused option of it (-1
// for none) and the items whose Suggest is running.
type deckFocus struct {
	item, opt int
	thinking  map[int]bool
}

// addNote is the click target shown after an option without a note.
const addNote = "+ note"

var riskColor = map[string]string{"low": "2", "med": "3", "high": "1"}

// wrapLines word-wraps text to width cells after prefix (the first line)
// and indent (the rest), styling the text with st.
func wrapLines(text, prefix, indent string, st lipgloss.Style, width int) []string {
	w := max(1, width-ansi.StringWidth(indent))
	var out []string
	for _, para := range strings.Split(text, "\n") {
		for _, l := range strings.Split(ansi.Wrap(para, w, ""), "\n") {
			p := indent
			if len(out) == 0 {
				p = prefix
			}
			out = append(out, trunc(p+st.Render(l), width))
		}
	}
	return out
}

// deckCounter is the first line of the deck region: `Deck: <title> ·
// <current>/<total>` in bold, and on the right, dim, `<answered>/<total>
// answered` and `sent`.
func (r *Renderer) deckCounter(f DeckFile, s DeckState, cur, width int) string {
	r.init()
	done, total := f.Deck.Answered(s)
	left := r.st.bold.Render(fmt.Sprintf("Deck: %s · %d/%d", f.Title(), min(cur+1, total), total))
	right := fmt.Sprintf("%d/%d answered", done, total)
	if f.Sent {
		right += " · sent"
	}
	return row(left, r.st.dim.Render(right), width)
}

// button is a clickable label: reversed in colour, in brackets without;
// off draws it darker (it would do nothing).
func (r *Renderer) button(label string, off bool) string {
	if !r.Color {
		return "[" + label + "]"
	}
	if off {
		return r.st.off.Render(" " + label + " ")
	}
	return r.st.bold.Reverse(true).Render(" " + label + " ")
}

// deckNav is the last line of the deck region: Back on the left (darker on
// the first decision), and on the right Next, Review on the last decision,
// Send on the review. It returns the line and the widths of both buttons.
func (r *Renderer) deckNav(cur, total, width int) (line string, backW, nextW int) {
	r.init()
	next := "Next ›"
	switch {
	case cur >= total:
		next = "Send"
	case cur == total-1:
		next = "Review ›"
	}
	back, fwd := r.button("‹ Back", cur == 0), r.button(next, false)
	backW, nextW = ansi.StringWidth(back), ansi.StringWidth(fwd)
	gap := max(1, width-backW-nextW)
	return back + strings.Repeat(" ", gap) + fwd, backW, nextW
}

// renderDeck lays out one page of the deck view: open item fc.item with
// its options and note (the first one after the deck's project, round,
// link, intro and the session's ack), or, at len(open items), the review.
func (r *Renderer) renderDeck(f DeckFile, s DeckState, fc deckFocus, width int) deckBody {
	r.init()
	d := &f.Deck
	b := deckBody{targets: map[int]deckTarget{}, noteX: map[int]int{}}
	add := func(t *deckTarget, lines ...string) {
		for _, l := range lines {
			if t != nil {
				b.targets[len(b.lines)] = *t
			}
			b.lines = append(b.lines, l)
		}
	}
	open := d.OpenItems()
	i := fc.item
	if i >= len(open) {
		r.renderReview(f, s, width, add)
		return b
	}
	if i == 0 {
		var meta []string
		if d.Project != "" {
			meta = append(meta, d.Project)
		}
		meta = append(meta, fmt.Sprintf("round %d", d.Round))
		if d.URL != "" {
			meta = append(meta, "↗ link (o)")
		}
		add(nil, r.st.dim.Render(trunc(strings.Join(meta, " · "), width)))
		if d.Intro != "" {
			add(nil, wrapLines(d.Intro, "", "", r.st.dim, width)...)
		}
		if f.Ack != "" {
			add(nil, wrapLines("Claude: "+f.Ack, "💬 ", "   ", r.st.noteExternal, width)...)
		}
		add(nil, "")
	}

	it := open[i]
	hdr := &deckTarget{item: i, opt: tgtHeader}
	add(hdr, row(r.st.accent.Render(fmt.Sprintf("%d. %s", it.N, it.Title)), r.pickLabel(it, s), width))
	if it.Summary != "" {
		add(hdr, wrapLines(it.Summary, "   ", "   ", lipgloss.NewStyle(), width)...)
	}
	if it.Details != "" {
		add(hdr, wrapLines(PlainText(it.Details), "   ", "   ", r.st.dim, width)...)
	}
	if it.URL != "" {
		u := r.st.dim.Render(trunc("↗ "+it.URL, width-3))
		if r.Color {
			u = ansi.SetHyperlink(hyperlinkURI(it.URL)) + u + ansi.ResetHyperlink()
		}
		add(&deckTarget{item: i, opt: tgtURL}, "   "+u)
	}
	for j, o := range OptionsFor(it, s) {
		t := &deckTarget{item: i, opt: j}
		dot, st := "○", lipgloss.NewStyle()
		if s.Choices[it.N] == o.Key {
			dot, st = r.st.ok.Render("●"), r.st.ok.Bold(true)
		}
		mark := "   "
		if j == fc.opt {
			mark = r.st.accent.Render(" › ")
		}
		left := fmt.Sprintf("%s%s %s  %s", mark, dot, st.Render(o.Key), st.Render(o.Title))
		if alias := confirmAlias[o.Key]; it.Kind == "confirm" && alias != "" {
			left += r.st.dim.Render("  " + alias)
		}
		if o.Key == it.Recommended {
			left += r.st.noteWaiting.Render("  ★ recommended")
		}
		if o.Generated {
			left += r.st.noteExternal.Render("  Generated")
		}
		on := strings.TrimSpace(s.OptNotes[it.N][o.Key])
		if on == "" {
			left += "  "
			b.noteX[len(b.lines)] = ansi.StringWidth(left)
			left += r.st.dim.Render(addNote)
		}
		add(t, row(left, r.badges(o), width))
		if o.Summary != "" {
			add(t, wrapLines(o.Summary, "        ", "        ", r.st.dim, width)...)
		}
		if len(o.Pros) > 0 {
			add(t, wrapLines("+ "+strings.Join(o.Pros, " · "), "        ", "          ", r.st.ok, width)...)
		}
		if len(o.Cons) > 0 {
			add(t, wrapLines("− "+strings.Join(o.Cons, " · "), "        ", "          ", r.st.err, width)...)
		}
		if o.Key == it.Recommended && it.Why != "" {
			add(t, wrapLines("why: "+it.Why, "        ", "             ", r.st.dim, width)...)
		}
		if on != "" {
			add(&deckTarget{item: i, opt: j, note: true}, wrapLines(on, "        ✎ ", "          ", lipgloss.NewStyle(), width)...)
		}
	}
	other := &deckTarget{item: i, opt: tgtOther}
	dot, st := "○", lipgloss.NewStyle()
	if s.Choices[it.N] == otherKey {
		dot, st = r.st.ok.Render("●"), r.st.ok.Bold(true)
	}
	add(other, fmt.Sprintf("   %s %s  %s", dot, st.Render("O"), st.Render("Other")))
	switch ot := strings.TrimSpace(s.Other[it.N]); {
	case ot != "":
		add(other, wrapLines(ot, "        ", "        ", lipgloss.NewStyle(), width)...)
	case s.Choices[it.N] == otherKey:
		add(other, r.st.dim.Render(trunc("        describe what you want instead (click, or O)", width)))
	}
	sug := "   ✦ Suggest another option (g)"
	if fc.thinking[it.N] {
		sug = "   ✦ Thinking…"
	}
	add(&deckTarget{item: i, opt: tgtSuggest}, r.st.dim.Render(trunc(sug, width)))
	add(&deckTarget{item: i, opt: tgtNote}, r.noteLines(s.Notes[it.N], width)...)
	return b
}

// pickLabel is an item's answer: `✓ <n><KEY>` (`✓ <n> Other`) in green, or
// a dim `open`.
func (r *Renderer) pickLabel(it DeckItem, s DeckState) string {
	if c := s.Choices[it.N]; c == otherKey {
		return r.st.ok.Render(fmt.Sprintf("✓ %d Other", it.N))
	} else if o, ok := optionFor(it, s, c); ok {
		return r.st.ok.Render(fmt.Sprintf("✓ %d%s", it.N, o.Key))
	}
	return r.st.dim.Render("open")
}

// renderReview is the page after the last decision: one row per open item
// with its pick or a yellow `open`, each a click target back to it, the
// items decided in earlier rounds, and the general note.
func (r *Renderer) renderReview(f DeckFile, s DeckState, width int, add func(*deckTarget, ...string)) {
	d := &f.Deck
	open := d.OpenItems()
	done, total := d.Answered(s)
	add(nil, r.st.accent.Render("Review before sending"))
	if done < total {
		add(nil, trunc(r.st.noteWaiting.Render(fmt.Sprintf("%d still open.", total-done))+r.st.dim.Render(" Click one to answer it, or send anyway."), width))
	} else {
		add(nil, r.st.dim.Render(trunc("Every decision has an answer.", width)))
	}
	add(nil, "")
	for i, it := range open {
		t := &deckTarget{item: i, opt: tgtGo}
		var line string
		if c := s.Choices[it.N]; c == otherKey {
			line = r.st.ok.Render(fmt.Sprintf("✓ %d Other", it.N)) + " — " + it.Title
		} else if o, ok := optionFor(it, s, c); ok {
			line = r.st.ok.Render(fmt.Sprintf("✓ %d%s", it.N, o.Key)) + " — " + it.Title + r.st.dim.Render(": "+o.Title)
		} else {
			line = r.st.noteWaiting.Render(fmt.Sprintf("○ %d", it.N)) + " — " + it.Title + "  " + r.st.noteWaiting.Render("open")
		}
		add(t, trunc(line, width))
	}
	var decided []DeckItem
	for _, it := range d.Items {
		if it.Decided != "" {
			decided = append(decided, it)
		}
	}
	if len(decided) > 0 {
		add(nil, "", r.st.bold.Render("Decided in earlier rounds"))
		for _, it := range decided {
			add(nil, wrapLines(it.Decided, "✓ ", "  ", r.st.dim, width)...)
		}
	}
	g := len(open)
	add(nil, "")
	add(&deckTarget{item: g, opt: tgtNote}, r.st.bold.Render("General notes"))
	add(&deckTarget{item: g, opt: tgtNote}, r.noteLines(s.General, width)...)
}

// confirmAlias is the extra key of each confirm option, shown after it.
var confirmAlias = map[string]string{"A": "y", "B": "r", "C": "f"}

// badges is an option's effort and risk, dim, the risk word coloured.
func (r *Renderer) badges(o DeckOption) string {
	var parts []string
	if o.Effort != "" {
		parts = append(parts, r.st.dim.Render("effort "+o.Effort))
	}
	if o.Risk != "" {
		st := r.st.dim
		if c, ok := riskColor[o.Risk]; ok && r.Color {
			st = lipgloss.NewStyle().Foreground(lipgloss.Color(c))
		}
		parts = append(parts, r.st.dim.Render("risk ")+st.Render(o.Risk))
	}
	return strings.Join(parts, r.st.dim.Render(" · "))
}

// noteLines is a note under `   ✎ `, or the dim invitation to write one.
func (r *Renderer) noteLines(note string, width int) []string {
	if strings.TrimSpace(note) == "" {
		return []string{r.st.dim.Render(trunc("   ✎ add a note (click, or n)", width))}
	}
	return wrapLines(strings.TrimSpace(note), "   ✎ ", "     ", lipgloss.NewStyle(), width)
}

// renderPicker is the `D` list when several decks exist: one row per deck,
// the selected one marked, with its answered count, `sent` or the ack.
func (r *Renderer) renderPicker(decks []DeckFile, sel, width int) []string {
	r.init()
	lines := []string{r.heading("🧭", "Decks", width)}
	for i, d := range decks {
		mark, ts := "  ", lipgloss.NewStyle()
		if i == sel {
			mark, ts = r.st.accent.Render("▸ "), r.st.accent
		}
		done, total := d.Deck.Answered(d.State)
		right := fmt.Sprintf("%d/%d", done, total)
		switch {
		case d.Ack != "":
			right = "💬 " + d.Ack
		case d.Sent:
			right = "sent"
		}
		lines = append(lines, row(mark+ts.Render(d.Title()), r.st.dim.Render(right), width))
	}
	return lines
}

// OnceDeck renders one uncolored frame of the deck view for the deck whose
// id (or file name without .json) is id: the fixed area, then the deck
// region at its first decision (counter, the whole decision, navigation).
func OnceDeck(s *Snapshot, id string, width int, now time.Time, o Options) (string, error) {
	r := &Renderer{Options: o}
	f := r.Render(s, width, now)
	if s.Missing {
		return f.Top + "\n", nil
	}
	for _, d := range s.Decks {
		if d.Err == nil && (d.Deck.ID == id || strings.TrimSuffix(d.Name, ".json") == id) {
			w := max(20, width)
			body := r.renderDeck(d, d.State, deckFocus{opt: -1}, w)
			nav, _, _ := r.deckNav(0, len(d.Deck.OpenItems()), w)
			return f.Top + "\n" + r.deckCounter(d, d.State, 0, w) + "\n" +
				strings.Join(append(body.lines, nav), "\n") + "\n", nil
		}
	}
	return "", fmt.Errorf("no deck %q in %s", id, s.Dir)
}
