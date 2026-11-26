package board

import (
	"os"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// screen is the view without colour, one string per row.
func screen(m *model) []string { return strings.Split(ansi.Strip(m.View()), "\n") }

// rowWith is the first screen row that contains s, or -1.
func rowWith(m *model, s string) int {
	for i, l := range screen(m) {
		if strings.Contains(l, s) {
			return i
		}
	}
	return -1
}

// One decision at a time: the counter, Next and Back by key, no advance on
// a pick, the review after the last decision, and enter there sends.
func TestDeckStepperKeys(t *testing.T) {
	m, _ := deckModel(t)
	m.openDeck(m.snap.Decks[0])
	if rowWith(m, "Deck: Panel build choices · 1/3") < 0 || rowWith(m, "1. Where deck files live") < 0 || rowWith(m, "2. Ship the R key") >= 0 {
		t.Fatalf("the deck should open on decision 1 alone:\n%s", ansi.Strip(m.View()))
	}
	deckPress(m, "b")
	if m.dv.cur != 0 || m.dv.state.Choices[1] != "B" {
		t.Errorf("a pick must not move on: cur %d, choices %v", m.dv.cur, m.dv.state.Choices)
	}
	deckPress(m, "backspace") // nothing before the first decision
	if m.dv.cur != 0 {
		t.Errorf("back on the first decision moved to %d", m.dv.cur)
	}
	deckPress(m, "enter")
	if m.dv.cur != 1 || rowWith(m, "Deck: Panel build choices · 2/3") < 0 || rowWith(m, "2. Ship the R key") < 0 {
		t.Errorf("enter should show decision 2:\n%s", ansi.Strip(m.View()))
	}
	deckPress(m, "shift+tab")
	if m.dv.cur != 0 {
		t.Errorf("shift+tab should go back to decision 1, at %d", m.dv.cur)
	}
	deckPress(m, "tab", "tab")
	if m.dv.cur != 2 || rowWith(m, "[Review ›]") < 0 {
		t.Errorf("the last decision's Next reads Review:\n%s", ansi.Strip(m.View()))
	}

	// j scrolls the page, it does not change the decision.
	deckPress(m, "j", "down")
	if m.dv.cur != 2 || m.dv.vp.YOffset != 2 {
		t.Errorf("j and down should scroll decision 3 by two lines: cur %d offset %d", m.dv.cur, m.dv.vp.YOffset)
	}

	deckPress(m, "enter")
	v := ansi.Strip(m.View())
	for _, want := range []string{"Review before sending", "1 still open.", "✓ 1B — Where deck files live: Anywhere, path in board.json",
		"✓ 2B — Ship the R key: Reject", "○ 3 — Note editor  open", "✓ 4A — Deck key", "General notes", "[Send]"} {
		if !strings.Contains(v, want) {
			t.Errorf("review lacks %q:\n%s", want, v)
		}
	}
	if m.dv.vp.YOffset != 0 {
		t.Errorf("a new page starts at its top, offset %d", m.dv.vp.YOffset)
	}
	deckPress(m, "a") // no decision on the review: option keys do nothing
	if m.dv.state.Choices[1] != "B" {
		t.Errorf("a on the review changed a pick: %v", m.dv.state.Choices)
	}
	deckPress(m, "n") // the general note, here
	if m.dv.editing != len(m.dv.open()) {
		t.Fatalf("n on the review should open the general note, editing %d", m.dv.editing)
	}
	m.dv.ta.SetValue("ship it")
	deckPress(m, "backspace", "enter") // in a text box backspace edits and enter saves
	if m.dv.state.General != "ship i" || m.dv.cur != 3 {
		t.Errorf("text box: general %q, cur %d", m.dv.state.General, m.dv.cur)
	}
	deckPress(m, "enter")
	if !m.dv.confirm {
		t.Fatal("enter on the review with an open item should ask first")
	}
	deckPress(m, "y")
	if _, err := os.Stat(AnswerBase(m.dir, "panel", 1) + ".txt"); err != nil {
		t.Errorf("y should send: %v", err)
	}

	deckPress(m, "u") // the next decision without a pick, from the review
	if m.dv.cur != 2 {
		t.Errorf("u should go to decision 3, at %d", m.dv.cur)
	}
}

// The split: the board above a third of the body, the deck region below,
// the whole body for the deck when the pane is short; an open text box
// keeps the split and the window's height.
func TestDeckStepperLayout(t *testing.T) {
	m, _ := deckModel(t) // 80x30: a body of 22 rows
	m.openDeck(m.snap.Decks[0])
	rows := screen(m)
	if len(rows) != 30 || !m.dv.split || m.vp.Height != 7 {
		t.Fatalf("split view: %d rows, split %v, board %d rows:\n%s", len(rows), m.dv.split, m.vp.Height, strings.Join(rows, "\n"))
	}
	if !strings.HasPrefix(rows[6], "📋 Plan") || !strings.HasPrefix(rows[14], "🙋 Deck: Panel build choices · 1/3") || rows[13] != strings.Repeat("─", 80) {
		t.Errorf("want the plan on row 6, a rule on 13, the counter on 14:\n%s", strings.Join(rows, "\n"))
	}
	if m.dv.navY != 27 || !strings.HasPrefix(rows[27], "[‹ Back]") || !strings.HasSuffix(rows[27], "[Next ›]") {
		t.Errorf("nav line on row %d:\n%s", m.dv.navY, strings.Join(rows, "\n"))
	}
	deckPress(m, "n")
	if rows = screen(m); len(rows) != 30 || !m.dv.split || !strings.HasPrefix(rows[6], "📋 Plan") {
		t.Errorf("an open text box should keep the split and 30 rows:\n%s", strings.Join(rows, "\n"))
	}
	deckPress(m, "esc")

	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24}) // a body of 16 rows
	rows = screen(m)
	if len(rows) != 24 || m.dv.split || !strings.HasPrefix(rows[6], "🙋 Deck: Panel build choices · 1/3") || rowWith(m, "📋 Plan") >= 0 {
		t.Errorf("a short pane gives the deck the whole body:\n%s", strings.Join(rows, "\n"))
	}
}

// Clicks on the nav buttons and on a review row; the wheel scrolls the
// part of the body it is over.
func TestDeckStepperMouse(t *testing.T) {
	m, _ := deckModel(t)
	m.openDeck(m.snap.Decks[0])
	press := func(x, y int) {
		m.Update(tea.MouseMsg{X: x, Y: y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	}
	press(79, m.dv.navY)
	if m.dv.cur != 1 {
		t.Errorf("Next should show decision 2, at %d", m.dv.cur)
	}
	press(1, m.dv.navY)
	if m.dv.cur != 0 {
		t.Errorf("Back should show decision 1, at %d", m.dv.cur)
	}
	press(40, m.dv.navY) // between the buttons: nothing
	if m.dv.cur != 0 {
		t.Errorf("a click between the buttons moved to %d", m.dv.cur)
	}

	m.goPage(3)
	for line, tg := range m.dv.body.targets {
		if tg == (deckTarget{item: 2, opt: tgtGo}) {
			clickLine(m, line, 2)
		}
	}
	if m.dv.cur != 2 {
		t.Errorf("a click on the review row of decision 3 should go there, at %d", m.dv.cur)
	}

	top := lipgloss.Height(m.top)
	m.vp.SetContent(strings.Repeat("x\n", 40)) // a board that scrolls
	m.Update(tea.MouseMsg{X: 5, Y: top + 1, Action: tea.MouseActionPress, Button: tea.MouseButtonWheelDown})
	if m.vp.YOffset == 0 || m.dv.vp.YOffset != 0 {
		t.Errorf("the wheel over the board scrolls the board: board %d, deck %d", m.vp.YOffset, m.dv.vp.YOffset)
	}
	m.Update(tea.MouseMsg{X: 5, Y: m.dv.navY - 1, Action: tea.MouseActionPress, Button: tea.MouseButtonWheelDown})
	if m.dv.vp.YOffset == 0 {
		t.Error("the wheel over the deck scrolls the decision")
	}
}
