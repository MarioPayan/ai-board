package board

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// refDeck and refState are the inputs the expected strings below were
// produced from, by running serialize() and sendDoc() of
// ~/.claude/skills/lib/artifacts/decision-deck.html in node on the same
// deck and state.
var refDeck = Deck{ID: "t", Title: "Test deck", Round: 2, Items: []DeckItem{
	{N: 1, Title: "Picked rec", Options: []DeckOption{{Key: "A", Title: "Alpha"}, {Key: "B", Title: "Beta"}}, Recommended: "A"},
	{N: 2, Title: "Picked other", Options: []DeckOption{{Key: "A", Title: "Alpha"}, {Key: "B", Title: "Beta"}}, Recommended: "A"},
	{N: 3, Title: "Left open", Options: []DeckOption{{Key: "A", Title: "Alpha"}}},
	{N: 4, Kind: "confirm", Title: "Confirm me"},
	{N: 5, Kind: "confirm", Title: "Reject me"},
	{N: 6, Title: "Old", Decided: "6A — Old: x"},
}}

func refState() DeckState {
	return DeckState{
		Choices: map[int]string{1: "A", 2: "B", 4: "A", 5: "B"},
		Notes:   map[int]string{2: "  first line\nsecond line\n  indented third \n", 3: "note on a skipped item", 4: "   "},
		General: "\n general one\ngeneral two  \n",
	}
}

const (
	refText = "DECISIONS · t · round 2\n# Test deck\n\n1A — Picked rec: Alpha (recommended)\n2B — Picked other: Beta\n   note: first line\n      second line\n        indented third\n3 skipped — Left open\n   note: note on a skipped item\n4A — Confirm me: Approve\n5B — Reject me: Reject\n\nGeneral notes:\n   general one\n   general two"
	refBare = "DECISIONS · t · round 2\n# Test deck\n\n1 skipped — Picked rec\n2 skipped — Picked other\n3 skipped — Left open\n4 skipped — Confirm me\n5 skipped — Reject me"
	// refText sent over refBare, which was sent at 15:00
	refResent = "DECISIONS · t · round 2\nRe-sent: replaces the answers sent 2026-11-24T15:00:00.000Z\n# Test deck\n\n1A — Picked rec: Alpha (recommended)\n2B — Picked other: Beta\n   note: first line\n      second line\n        indented third\n3 skipped — Left open\n   note: note on a skipped item\n4A — Confirm me: Approve\n5B — Reject me: Reject\n\nGeneral notes:\n   general one\n   general two"
	// refBare sent over refResent, which was sent at 16:00
	refAgain = "DECISIONS · t · round 2\nRe-sent: replaces the answers sent 2026-11-24T16:00:00.000Z\n# Test deck\n\n1 skipped — Picked rec\n2 skipped — Picked other\n3 skipped — Left open\n4 skipped — Confirm me\n5 skipped — Reject me"
)

func TestSerializeMatchesReference(t *testing.T) {
	if got := Serialize(&refDeck, refState()); got != refText {
		t.Errorf("choice, confirm, skipped, notes and general:\n--- got\n%s\n--- want\n%s", got, refText)
	}
	if got := Serialize(&refDeck, newDeckState()); got != refBare {
		t.Errorf("nothing picked:\n--- got\n%s\n--- want\n%s", got, refBare)
	}
}

func TestResendMatchesReference(t *testing.T) {
	at15 := time.Date(2026, 11, 24, 10, 0, 0, 0, time.FixedZone("-05", -5*3600))
	at16 := at15.Add(time.Hour)
	cases := []struct {
		name, text, prev string
		at               time.Time
		sent             bool
		want             string
		changed          bool
	}{
		{"first send", refText, "", time.Time{}, false, refText, true},
		{"other answers", refText, refBare, at15, true, refResent, true},
		{"same answers", refText, refText, at15, true, refText, false},
		{"over a re-sent round", refBare, refResent, at16, true, refAgain, true},
		{"same as the re-sent text", refText, refResent, at16, true, refResent, false},
	}
	for _, c := range cases {
		got, changed := Resend(c.text, c.prev, c.at, c.sent)
		if got != c.want || changed != c.changed {
			t.Errorf("%s: changed=%v\n--- got\n%s\n--- want (changed=%v)\n%s", c.name, changed, got, c.changed, c.want)
		}
	}
}

func TestSendAnswersFile(t *testing.T) {
	dir := t.TempDir()
	p := AnswerBase(dir, "t", 2) + ".txt"
	if _, err := SendAnswers(dir, &refDeck, newDeckState()); err != nil {
		t.Fatal(err)
	}
	sentAt := time.Date(2026, 11, 24, 15, 0, 0, 0, time.UTC)
	if err := os.Chtimes(p, sentAt, sentAt); err != nil {
		t.Fatal(err)
	}
	if _, err := SendAnswers(dir, &refDeck, newDeckState()); err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Stat(p); !fi.ModTime().Equal(sentAt) {
		t.Errorf("the same answers again rewrote the file (mtime %v)", fi.ModTime())
	}
	if _, err := SendAnswers(dir, &refDeck, refState()); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(p); string(b) != refResent {
		t.Errorf("re-sent file:\n%s\n--- want\n%s", b, refResent)
	}
	if ents, _ := os.ReadDir(filepath.Dir(p)); len(ents) != 1 {
		t.Errorf("temp files left behind: %v", ents)
	}
}

func TestDraftRoundTrip(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "decisions"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "decisions", "t.json"), []byte(`{"id":"t","title":"T","round":2,"items":[{"n":1,"title":"x","options":[{"key":"A","title":"a"}]}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	want := refState()
	if err := SaveDraft(AnswerBase(dir, "t", 2)+".draft.json", want); err != nil {
		t.Fatal(err)
	}
	if got := LoadDraft(AnswerBase(dir, "t", 2) + ".draft.json"); !reflect.DeepEqual(got, want) {
		t.Errorf("LoadDraft = %#v, want %#v", got, want)
	}
	s := Load(dir)
	if len(s.Decks) != 1 || !reflect.DeepEqual(s.Decks[0].State, want) || s.Decks[0].Sent {
		t.Errorf("Load did not pick up the draft: %#v", s.Decks)
	}
	if got := LoadDraft(filepath.Join(dir, "missing.json")); len(got.Choices) != 0 || got.Notes == nil {
		t.Errorf("missing draft = %#v, want an empty state", got)
	}
}

func TestFillRecommendedKeepsPicks(t *testing.T) {
	d := Deck{Items: []DeckItem{
		{N: 1, Options: []DeckOption{{Key: "A"}, {Key: "B"}}, Recommended: "A"},
		{N: 2, Options: []DeckOption{{Key: "A"}, {Key: "B"}}, Recommended: "A"},
		{N: 3, Options: []DeckOption{{Key: "A"}}},                                      // no recommendation
		{N: 4, Options: []DeckOption{{Key: "A"}}, Recommended: "Z"},                    // names no option
		{N: 5, Options: []DeckOption{{Key: "A"}}, Recommended: "A", Decided: "5A — x"}, // decided earlier
		{N: 6, Kind: "confirm", Recommended: "A"},
	}}
	s := newDeckState()
	s.Choices[1] = "B"
	if n := d.FillRecommended(s); n != 2 {
		t.Errorf("filled %d, want 2", n)
	}
	if want := map[int]string{1: "B", 2: "A", 6: "A"}; !reflect.DeepEqual(s.Choices, want) {
		t.Errorf("choices = %v, want %v", s.Choices, want)
	}
}

func TestPlainText(t *testing.T) {
	got := PlainText("<p>One &amp; <b>two</b></p>\n<ul><li>a</li><li>b</li></ul>x<br/>y")
	if want := "One & two\n• a\n• b\nx\ny"; got != want {
		t.Errorf("PlainText = %q, want %q", got, want)
	}
}

// deckModel is a live viewer on a copy of testdata/decisions, with the
// clipboard and the browser stubbed.
func deckModel(t *testing.T) (*model, *[]string) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "decisions")
	src := filepath.Join("..", "..", "testdata", "decisions")
	if err := os.CopyFS(dir, os.DirFS(src)); err != nil {
		t.Fatal(err)
	}
	var copied []string
	oldCopy, oldOpen := copyText, openURL
	copyText = func(s string) { copied = append(copied, s) }
	openURL = func(u string) error { copied = append(copied, "open "+u); return nil }
	t.Cleanup(func() { copyText, openURL = oldCopy, oldOpen })
	m := &model{dir: dir, r: &Renderer{}, snap: Load(dir), vp: viewport.New(80, 10)}
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 30})
	return m, &copied
}

func deckPress(m *model, keys ...string) {
	for _, k := range keys {
		msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
		switch k {
		case "enter":
			msg = tea.KeyMsg{Type: tea.KeyEnter}
		case "esc":
			msg = tea.KeyMsg{Type: tea.KeyEsc}
		}
		m.Update(msg)
	}
}

func lastLines(m *model, n int) string {
	lines := strings.Split(ansi.Strip(m.View()), "\n")
	return strings.Join(lines[len(lines)-n:], "\n")
}

func TestDeckViewKeys(t *testing.T) {
	m, copied := deckModel(t)
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("D")})
	if !m.picking || cmd == nil {
		t.Fatal("D with two decks should open the picker and enable the mouse")
	}
	if !strings.Contains(ansi.Strip(m.View()), "▸ Panel build choices") {
		t.Errorf("the picker should start on the unsent deck:\n%s", ansi.Strip(m.View()))
	}
	deckPress(m, "enter")
	if m.dv == nil || m.dv.file.Deck.ID != "panel" {
		t.Fatal("enter should open the selected deck")
	}
	if m.dv.state.Choices[1] != "A" || m.dv.state.Notes[1] != "keep it\nsimple" {
		t.Errorf("the draft was not loaded: %#v", m.dv.state)
	}
	if lines := strings.Split(m.View(), "\n"); len(lines) != 30 {
		t.Errorf("deck view is %d lines, want 30", len(lines))
	}

	deckPress(m, "u") // item 3, the only one without a pick
	if m.dv.cur != 2 {
		t.Errorf("u focused item %d, want index 2", m.dv.cur)
	}
	deckPress(m, "x") // nothing to clear on item 3; then clear item 1 and refill with R
	deckPress(m, "k", "k", "x")
	if _, ok := m.dv.state.Choices[1]; ok {
		t.Error("x should clear item 1")
	}
	deckPress(m, "j", "b", "R") // item 2: Approve→Reject is already B; b again clears it
	if m.dv.state.Choices[1] != "A" || m.dv.state.Choices[3] != "A" {
		t.Errorf("R should fill items 1 and 3 with A: %v", m.dv.state.Choices)
	}
	if _, ok := m.dv.state.Choices[2]; ok {
		t.Errorf("item 2 has no recommendation, R must leave it open: %v", m.dv.state.Choices)
	}
	draft := LoadDraft(m.draftPath(m.dv.file))
	if !reflect.DeepEqual(draft.Choices, m.dv.state.Choices) {
		t.Errorf("draft on disk %v, want %v", draft.Choices, m.dv.state.Choices)
	}

	deckPress(m, "s")
	if got := lastLines(m, 2); !strings.Contains(got, "1 open: send anyway? y/n") {
		t.Errorf("send with an open item should ask:\n%s", got)
	}
	deckPress(m, "n")
	answers := AnswerBase(m.dir, "panel", 1) + ".txt"
	if _, err := os.Stat(answers); err == nil {
		t.Fatal("n at the prompt must not send")
	}

	deckPress(m, "y") // item 2 is a confirm: y is Approve
	deckPress(m, "N")
	m.dv.ta.SetValue("all good")
	deckPress(m, "enter")
	if m.dv.state.General != "all good" || m.dv.editing != -1 {
		t.Errorf("general note not saved: %#v", m.dv.state)
	}
	deckPress(m, "s")
	b, err := os.ReadFile(answers)
	if err != nil {
		t.Fatal("s with every item picked should send: ", err)
	}
	want := "DECISIONS · panel · round 1\n# Panel build choices\n\n1A — Where deck files live: decisions/ in the board dir (recommended)\n   note: keep it\n      simple\n2A — Ship the R key: Approve\n3A — Note editor: Inline text box (recommended)\n\nGeneral notes:\n   all good"
	if string(b) != want {
		t.Errorf("answers file:\n%s\n--- want\n%s", b, want)
	}
	if len(*copied) != 1 || (*copied)[0] != want {
		t.Errorf("clipboard got %q", *copied)
	}
	if got := lastLines(m, 2); !strings.Contains(got, "✓ sent 3/3 · copied") {
		t.Errorf("footer after send:\n%s", got)
	}
	if strings.Contains(ansi.Strip(m.top), "Deck: Panel build choices") {
		t.Errorf("a sent deck leaves the header slots:\n%s", ansi.Strip(m.top))
	}

	deckPress(m, "o") // focused item 2 has no url: the deck's
	if (*copied)[len(*copied)-1] != "open https://claude.ai/artifact/example" {
		t.Errorf("o opened %q", (*copied)[len(*copied)-1])
	}
	_, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if m.dv != nil || cmd == nil {
		t.Error("esc should close the deck view and disable the mouse")
	}
}

func TestDeckViewMouse(t *testing.T) {
	m, _ := deckModel(t)
	m.openDeck(m.snap.Decks[0])
	click := func(target deckTarget) {
		t.Helper()
		for line, tg := range m.dv.body.targets {
			if tg == target {
				m.vp.SetYOffset(line)
				m.Update(tea.MouseMsg{X: 5, Y: 6 + line - m.vp.YOffset, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
				return
			}
		}
		t.Fatalf("no line targets %v", target)
	}
	click(deckTarget{2, 1}) // item 3, option B
	if m.dv.state.Choices[3] != "B" {
		t.Errorf("click should pick 3B: %v", m.dv.state.Choices)
	}
	click(deckTarget{2, 1})
	if _, ok := m.dv.state.Choices[3]; ok {
		t.Error("a second click should clear the pick")
	}
	click(deckTarget{0, tgtNote})
	if m.dv.editing != 0 || m.dv.ta.Value() != "keep it\nsimple" {
		t.Errorf("clicking the note should open it: editing %d %q", m.dv.editing, m.dv.ta.Value())
	}
	deckPress(m, "esc")
	m.Update(tea.MouseMsg{X: 1, Y: 29, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	if !m.dv.confirm {
		t.Error("clicking Send with open items should ask first")
	}
}

func TestDeckAckShown(t *testing.T) {
	m, _ := deckModel(t)
	m.openDeck(m.snap.Decks[1])
	if v := ansi.Strip(m.View()); !strings.Contains(v, "💬 Claude: action plan coming") {
		t.Errorf("the ack should show in the deck view:\n%s", v)
	}
}
