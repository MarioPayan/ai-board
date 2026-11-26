package board

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// The web deck template whose logic the deck view must match byte for
// byte: $AI_BOARD_DECK_TEMPLATE, or the installed skills copy.
func deckTemplate(t *testing.T) string {
	t.Helper()
	if p := os.Getenv("AI_BOARD_DECK_TEMPLATE"); p != "" {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".claude", "skills", "lib", "artifacts", "decision-deck.html")
}

// templateHarness runs after the template's logic script: it reads
// {deck, state} on stdin and prints what serialize(), nextKey() and
// progress() return.
const templateHarness = `
let input = "";
process.stdin.on("data", d => input += d).on("end", () => {
  const { deck, state: s } = JSON.parse(input);
  const state = Object.assign(emptyState(), s);
  for (const k of ["choices", "other", "notes", "optNotes", "extra"]) state[k] = state[k] || {};
  const keys = {};
  for (const item of deck.items) keys[item.n] = nextKey(item, state);
  const p = progress(deck, state);
  process.stdout.write(JSON.stringify({ text: serialize(deck, state), keys, done: p.done, total: p.total }));
});
`

type templateResult struct {
	Text  string            `json:"text"`
	Keys  map[string]string `json:"keys"`
	Done  int               `json:"done"`
	Total int               `json:"total"`
}

// runTemplate extracts <script id="logic"> from the template and runs it in
// node on deck and state.
func runTemplate(t *testing.T, node, tmpl string, d *Deck, s DeckState) templateResult {
	t.Helper()
	html, err := os.ReadFile(tmpl)
	if err != nil {
		t.Fatal(err)
	}
	_, logic, ok := strings.Cut(string(html), `<script id="logic">`)
	if logic, _, ok = strings.Cut(logic, "</script>"); !ok {
		t.Fatalf("%s: no <script id=\"logic\"> … </script>", tmpl)
	}
	js := filepath.Join(t.TempDir(), "deck-logic.cjs")
	if err := os.WriteFile(js, []byte(logic+templateHarness), 0o644); err != nil {
		t.Fatal(err)
	}
	in, err := json.Marshal(map[string]any{"deck": d, "state": s})
	if err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	cmd := exec.Command(node, js)
	cmd.Stdin, cmd.Stderr = bytes.NewReader(in), &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("node %s: %v\n%s", js, err, stderr.String())
	}
	var r templateResult
	if err := json.Unmarshal(out, &r); err != nil {
		t.Fatalf("template output %q: %v", out, err)
	}
	return r
}

// TestParityWithTemplate runs the web deck's own serialize(), nextKey() and
// progress() on the parity deck and compares them with Serialize, NextKey
// and Answered, so drift in the template's answer grammar fails here.
func TestParityWithTemplate(t *testing.T) {
	node, err := exec.LookPath("node")
	tmpl := deckTemplate(t)
	if _, statErr := os.Stat(tmpl); err != nil || statErr != nil {
		t.Skip("node or template missing (set AI_BOARD_DECK_TEMPLATE to the decision-deck.html to check)")
	}
	for _, c := range []struct {
		name  string
		state DeckState
	}{
		{"parity state", parityState()},
		{"empty state", newDeckState()},
	} {
		t.Run(c.name, func(t *testing.T) {
			r := runTemplate(t, node, tmpl, &parityDeck, c.state)
			if got := Serialize(&parityDeck, c.state); got != r.Text {
				t.Errorf("Serialize differs from the template's serialize():\n--- go\n%s\n--- template\n%s", got, r.Text)
			}
			for _, it := range parityDeck.Items {
				if got, want := NextKey(it, c.state), r.Keys[strconv.Itoa(it.N)]; got != want {
					t.Errorf("NextKey(item %d) = %q, template nextKey() = %q", it.N, got, want)
				}
			}
			if done, total := parityDeck.Answered(c.state); done != r.Done || total != r.Total {
				t.Errorf("Answered = %d/%d, template progress() = %d/%d", done, total, r.Done, r.Total)
			}
		})
	}
	// the hard-coded reference the other parity tests use is the template's
	// output too, so those tests cannot drift from it unnoticed
	if r := runTemplate(t, node, tmpl, &parityDeck, parityState()); r.Text != parityText {
		t.Errorf("parityText is stale; the template now serializes:\n%s", r.Text)
	}
}
