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
	tgtHeader  = -1 // focus the item
	tgtNote    = -2 // edit its note
	tgtURL     = -3 // open its url
	tgtOther   = -4 // pick Other and edit its text
	tgtSuggest = -5 // ask Claude for another option
)

// deckTarget is a click target. With note set, opt is an option index and
// the click edits the note on that option.
type deckTarget struct {
	item, opt int
	note      bool
}

// deckBody is a rendered deck: its lines, the click target of each line
// that has one, the first line of every open item (and, last, of the
// general note), and, on option lines that show `+ note`, the column where
// it starts.
type deckBody struct {
	lines    []string
	targets  map[int]deckTarget
	itemLine []int
	noteX    map[int]int
}

// deckFocus is what the deck view highlights: the focused item, the
// focused option of it (-1 for none) and the items whose Suggest is
// running.
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

// renderDeck lays out a deck view body: the deck title, its intro, the
// session's ack, earlier decisions, then every open item with its options
// and note, and the general note. cur is the focused item.
func (r *Renderer) renderDeck(f DeckFile, s DeckState, fc deckFocus, width int) deckBody {
	r.init()
	cur := fc.item
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
	done, total := d.Answered(s)
	add(nil, r.heading("🧭", f.Title(), width))
	var meta []string
	if d.Project != "" {
		meta = append(meta, d.Project)
	}
	meta = append(meta, fmt.Sprintf("round %d", d.Round), fmt.Sprintf("%d/%d answered", done, total))
	if f.Sent {
		meta = append(meta, "sent")
	}
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

	open := d.OpenItems()
	b.itemLine = make([]int, len(open)+1)
	focus := func(i int) (string, lipgloss.Style) {
		if i == cur {
			return r.st.accent.Render("▸ "), r.st.accent
		}
		return "  ", r.st.bold
	}
	for i, it := range open {
		add(nil, "")
		b.itemLine[i] = len(b.lines)
		hdr := &deckTarget{item: i, opt: tgtHeader}
		mark, ts := focus(i)
		pick := r.st.dim.Render("open")
		if c := s.Choices[it.N]; c == otherKey {
			pick = r.st.ok.Render(fmt.Sprintf("✓ %d Other", it.N))
		} else if o, ok := optionFor(it, s, c); ok {
			pick = r.st.ok.Render(fmt.Sprintf("✓ %d%s", it.N, o.Key))
		}
		add(hdr, row(mark+ts.Render(fmt.Sprintf("%d. %s", it.N, it.Title)), pick, width))
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
			if i == cur && j == fc.opt {
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
	}
	add(nil, "")
	g := len(open)
	b.itemLine[g] = len(b.lines)
	mark, ts := focus(g)
	add(&deckTarget{item: g, opt: tgtHeader}, mark+ts.Render("General notes"))
	add(&deckTarget{item: g, opt: tgtNote}, r.noteLines(s.General, width)...)
	return b
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
// id (or file name without .json) is id: the fixed area, then the deck.
func OnceDeck(s *Snapshot, id string, width int, now time.Time, o Options) (string, error) {
	r := &Renderer{Options: o}
	f := r.Render(s, width, now)
	if s.Missing {
		return f.Top + "\n", nil
	}
	for _, d := range s.Decks {
		if d.Err == nil && (d.Deck.ID == id || strings.TrimSuffix(d.Name, ".json") == id) {
			body := r.renderDeck(d, d.State, deckFocus{opt: -1}, max(20, width))
			return f.Top + "\n" + strings.TrimRight(strings.Join(body.lines, "\n"), "\n") + "\n", nil
		}
	}
	return "", fmt.Errorf("no deck %q in %s", id, s.Dir)
}
