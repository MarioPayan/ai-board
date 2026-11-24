package board

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// parityDeck and parityState are the inputs the expected strings below were
// produced from, by running serialize(), nextKey() and the Suggest prompt
// (context() plus the instruction) of
// ~/.claude/skills/lib/artifacts/decision-deck.html in node on the same deck
// and state.
var parityDeck = Deck{ID: "p", Title: "Parity deck", Project: "ai-board", Round: 3, Items: []DeckItem{
	{N: 1, Title: "Other with text", Options: []DeckOption{{Key: "A", Title: "Alpha"}, {Key: "B", Title: "Beta"}}, Recommended: "A"},
	{N: 2, Title: "Other empty", Options: []DeckOption{{Key: "A", Title: "Alpha"}}},
	{N: 3, Kind: "confirm", Title: "Other on confirm"},
	{N: 4, Title: "Notes on options", Summary: "Pick one.", Options: []DeckOption{{Key: "A", Title: "Alpha", Summary: "First."}, {Key: "B", Title: "Beta"}, {Key: "C", Title: "Gamma"}}, Recommended: "B"},
	{N: 5, Title: "Notes, nothing picked", Options: []DeckOption{{Key: "A", Title: "Alpha"}, {Key: "B", Title: "Beta"}}},
	{N: 6, Title: "Generated picked", Options: []DeckOption{{Key: "A", Title: "Alpha"}, {Key: "B", Title: "Beta"}}, Recommended: "A"},
	{N: 7, Title: "Generated, no summary", Options: []DeckOption{{Key: "A", Title: "Alpha"}}},
	{N: 8, Kind: "confirm", Title: "Generated on confirm, not picked"},
	{N: 9, Title: "Old", Decided: "9A — Old: x"},
}}

func parityState() DeckState {
	return DeckState{
		Choices: map[int]string{1: "other", 2: "other", 3: "other", 4: "B", 6: "C", 7: "B", 8: "A"},
		Other:   map[int]string{1: "  something else\n entirely ", 3: "approve, but only on staging", 5: "ignored: not picked"},
		Notes:   map[int]string{1: "item note after other", 4: "item note\nsecond line", 6: "why C"},
		OptNotes: map[int]map[string]string{
			4: {"C": "  on gamma\nmore ", "A": "on alpha", "B": "   "},
			5: {"B": "on beta only"},
			6: {"C": "on the generated one", "A": "on alpha"},
			8: {"D": "note on a generated confirm option", "B": "reject note"},
		},
		Extra: map[int][]DeckOption{
			6: {{Key: "C", Title: "Gamma idea", Summary: "  Do the third thing.\nWith a second line. ", Pros: []string{"p"}, Cons: []string{}, Effort: "S", Risk: "low", Generated: true}},
			7: {{Key: "B", Title: "No summary", Pros: []string{}, Cons: []string{}, Generated: true}},
			8: {{Key: "D", Title: "Approve later", Summary: "Wait a week.", Pros: []string{}, Cons: []string{}, Effort: "M", Risk: "med", Generated: true}},
		},
		General: "  all done\n  indented general ",
	}
}

const (
	parityText = "DECISIONS · p · round 3\n# Parity deck\n\n1 Other — Other with text: \"something else\n entirely\"\n   note: item note after other\n2 Other — Other empty: \"\"\n3 Other — Other on confirm: \"approve, but only on staging\"\n4B — Notes on options: Beta (recommended)\n   note: item note\n      second line\n   note on A: on alpha\n   note on C: on gamma\n      more\n5 skipped — Notes, nothing picked\n   note on B: on beta only\n6C — Generated picked: Gamma idea (generated in page)\n   option: Do the third thing.\n      With a second line.\n   note: why C\n   note on A: on alpha\n   note on C: on the generated one\n7B — Generated, no summary: No summary (generated in page)\n   option: \n8A — Generated on confirm, not picked: Approve\n   note on B: reject note\n   note on D: note on a generated confirm option\n\nGeneral notes:\n   all done\n     indented general"
	// the Suggest prompt for item 6
	parityPrompt = "Deck: Parity deck (project: ai-board)\nItem 6: Generated picked\n\nOptions:\nA. Alpha — \nB. Beta — \nC. Gamma idea —   Do the third thing.\nWith a second line. \n\nPropose ONE new option that differs from all of the above. Reply as JSON: {\"title\": string (max 6 words), \"summary\": string (one sentence), \"pros\": string[], \"cons\": string[], \"effort\": \"S\"|\"M\"|\"L\", \"risk\": \"low\"|\"med\"|\"high\"}"
)

func TestSerializeOtherOptionNotesGeneratedMatchReference(t *testing.T) {
	if got := Serialize(&parityDeck, parityState()); got != parityText {
		t.Errorf("Other, notes on options and generated options:\n--- got\n%s\n--- want\n%s", got, parityText)
	}
	if done, total := parityDeck.Answered(parityState()); done != 7 || total != 8 {
		t.Errorf("Answered = %d/%d, want 7/8 (Other counts as a pick)", done, total)
	}
}

func TestSuggestPromptAndNextKeyMatchReference(t *testing.T) {
	s := parityState()
	if got := suggestPrompt(&parityDeck, parityDeck.Items[5], s); got != parityPrompt {
		t.Errorf("prompt:\n--- got\n%q\n--- want\n%q", got, parityPrompt)
	}
	for _, c := range []struct {
		item int
		want string
	}{{6, "D"}, {8, "E"}, {3, "D"}, {1, "C"}} {
		if got := NextKey(parityDeck.Items[c.item-1], s); got != c.want {
			t.Errorf("NextKey(item %d) = %q, want %q", c.item, got, c.want)
		}
	}
}

func TestParseSuggestion(t *testing.T) {
	env := func(result string) []byte {
		b, _ := json.Marshal(map[string]any{"type": "result", "is_error": false, "result": result})
		return b
	}
	got, err := parseSuggestion(env("Here it is:\n```json\n{\"title\": \"Ship it\", \"summary\": \"Now.\", \"pros\": \"fast\", \"cons\": [\"risky\", 2], \"effort\": 0, \"risk\": \"high\"}\n```"))
	want := DeckOption{Title: "Ship it", Summary: "Now.", Pros: []string{"fast"}, Cons: []string{"risky", "2"}, Effort: "", Risk: "high", Generated: true}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Errorf("fenced reply = %#v, %v; want %#v", got, err, want)
	}
	for _, c := range []struct {
		name string
		out  []byte
		code string
	}{
		{"not an envelope", []byte("oops"), "error"},
		{"error result", []byte(`{"is_error": true, "result": "{\"title\": \"x\"}"}`), "error"},
		{"no object", env("I cannot."), "bad_json"},
		{"title not a string", env(`{"title": 3}`), "bad_shape"},
	} {
		if _, err := parseSuggestion(c.out); suggestFailText(err) != "No option came back ("+c.code+"). Try again." {
			t.Errorf("%s: %v, want code %s", c.name, err, c.code)
		}
	}
}

// fakeClaude replaces the `claude -p` runner with one that records the
// prompt and returns reply (or fails with err).
func fakeClaude(t *testing.T, reply string, err error) *[]string {
	t.Helper()
	var prompts []string
	old := runClaude
	runClaude = func(_ context.Context, prompt string) ([]byte, error) {
		prompts = append(prompts, prompt)
		if err != nil {
			return nil, err
		}
		return json.Marshal(map[string]any{"type": "result", "is_error": false, "result": reply})
	}
	t.Cleanup(func() { runClaude = old })
	return &prompts
}

// deckText is the whole deck view body, not only the lines on screen.
func deckText(m *model) string { return ansi.Strip(strings.Join(m.dv.body.lines, "\n")) }

func TestDeckViewOtherOptionNoteSuggest(t *testing.T) {
	m, copied := deckModel(t)
	m.openDeck(m.snap.Decks[0])
	v := deckText(m)
	for _, want := range []string{"○ O  Other", "✦ Suggest another option (g)", "○ C  Note in the header slot  Generated  + note", "✎ only if decks get shared"} {
		if !strings.Contains(v, want) {
			t.Errorf("deck view lacks %q:\n%s", want, v)
		}
	}

	// Other on item 2: O picks it and opens the box; enter saves the text.
	deckPress(m, "j", "O")
	if m.dv.state.Choices[2] != "other" || m.dv.editing != 1 || m.dv.editWhat != editOther {
		t.Fatalf("O should pick Other on item 2 and open its box: %#v editing %d", m.dv.state.Choices, m.dv.editing)
	}
	if got := lastLines(m, 6); !strings.Contains(got, "Other for 2. Ship the R key") {
		t.Errorf("the box should name Other:\n%s", got)
	}
	m.dv.ta.SetValue("  approve after the demo ")
	deckPress(m, "enter")
	if m.dv.state.Other[2] != "approve after the demo" || !strings.Contains(deckText(m), "✓ 2 Other") {
		t.Errorf("Other text not saved or shown: %#v", m.dv.state.Other)
	}

	// A note on option C of item 2 (Defer), which is not picked: → → → e.
	deckPress(m, "right", "right", "right", "e")
	if m.dv.editWhat != editOptNote || m.dv.editKey != "C" {
		t.Fatalf("e should open the note on the focused option C: %v %q", m.dv.editWhat, m.dv.editKey)
	}
	m.dv.ta.SetValue("defer is fine too")
	deckPress(m, "enter")
	if m.dv.state.OptNotes[2]["C"] != "defer is fine too" {
		t.Errorf("option note not saved: %#v", m.dv.state.OptNotes)
	}

	// Suggest on item 3: Thinking… until the reply arrives, then option D.
	prompts := fakeClaude(t, `{"title": "Popup editor", "summary": "A modal box.", "pros": ["Roomy"], "cons": ["Modal"], "effort": "M", "risk": "med"}`, nil)
	deckPress(m, "j")
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("g")})
	if cmd == nil || !strings.Contains(deckText(m), "✦ Thinking…") {
		t.Fatalf("g should start Suggest and show Thinking…:\n%s", deckText(m))
	}
	if _, again := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("g")}); again != nil {
		t.Error("a second g while thinking must not start another call")
	}
	m.Update(cmd())
	if len(*prompts) != 1 || !strings.HasPrefix((*prompts)[0], "Deck: Panel build choices (project: ai-board)\nItem 3: Note editor\n") ||
		!strings.Contains((*prompts)[0], "\nC. Note in the header slot — Show the note under the deck row.\n\nPropose ONE") {
		t.Errorf("prompt %q", *prompts)
	}
	ex := m.dv.state.Extra[3]
	if len(ex) != 2 || ex[1].Key != "D" || !ex[1].Generated || ex[1].Title != "Popup editor" {
		t.Fatalf("the generated option should be 3D: %#v", ex)
	}
	if v := deckText(m); strings.Contains(v, "Thinking…") || !strings.Contains(v, "○ D  Popup editor  Generated  + note") {
		t.Errorf("view after Suggest:\n%s", v)
	}
	if d := LoadDraft(m.draftPath(m.dv.file)); len(d.Extra[3]) != 2 || d.Other[2] != "approve after the demo" || d.OptNotes[2]["C"] == "" {
		t.Errorf("draft on disk lost the new fields: %#v", d)
	}

	// A failing Suggest says so in the status line, in the template's words.
	fakeClaude(t, "", errors.New("exit 1"))
	_, cmd = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("g")})
	m.Update(cmd())
	if got := lastLines(m, 2); !strings.Contains(got, "No option came back (error). Try again.") {
		t.Errorf("failure wording:\n%s", got)
	}

	deckPress(m, "d", "s", "y") // pick 3D, send
	want := "DECISIONS · panel · round 1\n# Panel build choices\n\n1A — Where deck files live: decisions/ in the board dir (recommended)\n   note: keep it\n      simple\n   note on B: only if decks get shared\n2 Other — Ship the R key: \"approve after the demo\"\n   note on C: defer is fine too\n3D — Note editor: Popup editor (generated in page)\n   option: A modal box."
	b, err := os.ReadFile(AnswerBase(m.dir, "panel", 1) + ".txt")
	if err != nil || string(b) != want {
		t.Errorf("answers file (%v):\n%s\n--- want\n%s", err, b, want)
	}
	if len(*copied) == 0 || (*copied)[len(*copied)-1] != want {
		t.Errorf("clipboard %q", *copied)
	}
}

func TestDeckViewMouseOtherNoteSuggest(t *testing.T) {
	m, _ := deckModel(t)
	m.openDeck(m.snap.Decks[0])
	clickAt := func(target deckTarget, x func(line int) int) {
		t.Helper()
		for line := range len(m.dv.body.lines) {
			if tg, ok := m.dv.body.targets[line]; ok && tg == target {
				m.vp.SetYOffset(line)
				m.Update(tea.MouseMsg{X: x(line), Y: 6 + line - m.vp.YOffset, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
				return
			}
		}
		t.Fatalf("no line targets %v", target)
	}
	at := func(int) int { return 5 }
	clickAt(deckTarget{item: 2, opt: tgtOther}, at)
	if m.dv.state.Choices[3] != "other" || m.dv.editWhat != editOther || m.dv.editing != 2 {
		t.Errorf("clicking Other should pick it and open its box: %v", m.dv.state.Choices)
	}
	deckPress(m, "esc")
	// `+ note` on 3B: the click lands on the label, not the row.
	clickAt(deckTarget{item: 2, opt: 1}, func(line int) int { return m.dv.body.noteX[line] + 1 })
	if m.dv.editWhat != editOptNote || m.dv.editKey != "B" || m.dv.state.Choices[3] != "other" {
		t.Errorf("clicking + note should open the note on 3B without picking it: %v %q %v", m.dv.editWhat, m.dv.editKey, m.dv.state.Choices)
	}
	m.dv.ta.SetValue("too slow")
	deckPress(m, "enter")
	// the note now shows under 3B; clicking it opens it again
	clickAt(deckTarget{item: 2, opt: 1, note: true}, at)
	if m.dv.editWhat != editOptNote || m.dv.ta.Value() != "too slow" {
		t.Errorf("clicking the option note should reopen it: %q", m.dv.ta.Value())
	}
	deckPress(m, "esc")
	fakeClaude(t, `{"title": "x"}`, nil)
	clickAt(deckTarget{item: 2, opt: tgtSuggest}, at)
	if !m.dv.thinking[3] {
		t.Error("clicking Suggest should start it")
	}
}

func TestSuggestionAfterLeavingTheDeck(t *testing.T) {
	m, _ := deckModel(t)
	m.openDeck(m.snap.Decks[0])
	fakeClaude(t, `{"title": "Later"}`, nil)
	deckPress(m, "j", "j")
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("g")})
	deckPress(m, "esc")
	m.Update(cmd())
	d := LoadDraft(AnswerBase(m.dir, "panel", 1) + ".draft.json")
	if ex := d.Extra[3]; len(ex) != 2 || ex[1].Key != "D" || ex[1].Title != "Later" {
		t.Errorf("a reply after leaving the deck should land in its draft: %#v", ex)
	}
}

func TestDeckFooterFits80(t *testing.T) {
	if w := sendW + ansi.StringWidth(deckKeys); w > 80 {
		t.Errorf("deck footer keys line is %d cells, want at most 80", w)
	}
}
