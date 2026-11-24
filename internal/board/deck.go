package board

import (
	"encoding/json"
	"fmt"
	"html"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// Deck is a decisions/<id>.json file: the web decision deck's JSON, read
// unchanged. Mockups (`before`, `mock`) are parsed and never rendered.
type Deck struct {
	ID      string     `json:"id"`
	Title   string     `json:"title"`
	Project string     `json:"project"`
	Round   int        `json:"round"`
	Intro   string     `json:"intro"`
	URL     string     `json:"url"`
	Items   []DeckItem `json:"items"`
}

// DeckItem is one numbered decision.
type DeckItem struct {
	N           int          `json:"n"`
	Title       string       `json:"title"`
	Summary     string       `json:"summary"`
	Details     string       `json:"details"` // trusted HTML, shown as plain text
	Kind        string       `json:"kind"`    // "choice" (default) or "confirm"
	Before      string       `json:"before"`
	Options     []DeckOption `json:"options"`
	Recommended string       `json:"recommended"`
	Why         string       `json:"why"`
	Decided     string       `json:"decided"` // an answer line from an earlier round
	URL         string       `json:"url"`
}

// DeckOption is one option of a choice item.
type DeckOption struct {
	Key     string   `json:"key"`
	Title   string   `json:"title"`
	Summary string   `json:"summary"`
	Pros    []string `json:"pros"`
	Cons    []string `json:"cons"`
	Effort  string   `json:"effort"`
	Risk    string   `json:"risk"`
	Mock    string   `json:"mock"`
}

// confirmOptions are the options of a `kind: confirm` item, with the keys
// the web template gives them.
var confirmOptions = []DeckOption{{Key: "A", Title: "Approve"}, {Key: "B", Title: "Reject"}, {Key: "C", Title: "Defer"}}

// OptionsList is the item's options: Approve / Reject / Defer for a confirm item.
func (it DeckItem) OptionsList() []DeckOption {
	if it.Kind == "confirm" {
		return confirmOptions
	}
	return it.Options
}

func (it DeckItem) option(key string) (DeckOption, bool) {
	for _, o := range it.OptionsList() {
		if o.Key == key {
			return o, true
		}
	}
	return DeckOption{}, false
}

// OpenItems is every item not decided in an earlier round, in file order.
func (d *Deck) OpenItems() []DeckItem {
	var out []DeckItem
	for _, it := range d.Items {
		if it.Decided == "" {
			out = append(out, it)
		}
	}
	return out
}

// DeckState is the answers being drafted: the option key picked per item
// number, the item notes and the general note. It is the draft file's
// shape, a subset of the web template's state.
type DeckState struct {
	Choices map[int]string `json:"choices"`
	Notes   map[int]string `json:"notes"`
	General string         `json:"general"`
}

func newDeckState() DeckState {
	return DeckState{Choices: map[int]string{}, Notes: map[int]string{}}
}

// Answered counts the open items with a pick.
func (d *Deck) Answered(s DeckState) (done, total int) {
	for _, it := range d.OpenItems() {
		total++
		if _, ok := it.option(s.Choices[it.N]); ok {
			done++
		}
	}
	return done, total
}

// FillRecommended picks the recommended option of every open item that has
// none yet and returns how many it filled. It never replaces a pick.
func (d *Deck) FillRecommended(s DeckState) int {
	n := 0
	for _, it := range d.OpenItems() {
		if _, picked := it.option(s.Choices[it.N]); picked || it.Recommended == "" {
			continue
		}
		if _, ok := it.option(it.Recommended); ok {
			s.Choices[it.N] = it.Recommended
			n++
		}
	}
	return n
}

// noteLines is the web template's indent(): the trimmed text's first line
// after `   <label>: `, the rest indented six spaces.
func noteLines(label, text string) []string {
	lines := strings.Split(strings.TrimSpace(text), "\n")
	out := []string{"   " + label + ": " + lines[0]}
	for _, l := range lines[1:] {
		out = append(out, "      "+l)
	}
	return out
}

// Serialize is the answer text, byte for byte what the web template's
// serialize() produces for the same picks and notes (see decision-deck.md,
// Read the answers): no trailing newline.
func Serialize(d *Deck, s DeckState) string {
	out := []string{fmt.Sprintf("DECISIONS · %s · round %d", d.ID, d.Round), "# " + d.Title, ""}
	for _, it := range d.OpenItems() {
		o, ok := it.option(s.Choices[it.N])
		if !ok {
			out = append(out, fmt.Sprintf("%d skipped — %s", it.N, it.Title))
		} else {
			flag := ""
			if o.Key == it.Recommended {
				flag = " (recommended)"
			}
			out = append(out, fmt.Sprintf("%d%s — %s: %s%s", it.N, o.Key, it.Title, o.Title, flag))
		}
		if strings.TrimSpace(s.Notes[it.N]) != "" {
			out = append(out, noteLines("note", s.Notes[it.N])...)
		}
	}
	if g := strings.TrimSpace(s.General); g != "" {
		out = append(out, "", "General notes:")
		for _, l := range strings.Split(g, "\n") {
			out = append(out, "   "+l)
		}
	}
	return strings.Join(out, "\n")
}

// sentAtFormat is JavaScript's Date.toISOString(), the web template's
// sentAt.
const sentAtFormat = "2006-01-02T15:04:05.000Z"

// Resend is what a send writes when the round was already sent as prev at
// prevAt: text itself when nothing was sent, prev unchanged (changed false)
// when the answers are the same, otherwise text with line 2
// `Re-sent: replaces the answers sent <ISO>`, like the web template's
// sendDoc().
func Resend(text, prev string, prevAt time.Time, sentBefore bool) (out string, changed bool) {
	if !sentBefore {
		return text, true
	}
	pl := strings.Split(prev, "\n")
	if len(pl) > 1 && strings.HasPrefix(pl[1], "Re-sent: ") {
		pl = append(pl[:1], pl[2:]...)
	}
	if strings.Join(pl, "\n") == text {
		return prev, false
	}
	lines := strings.Split(text, "\n")
	re := "Re-sent: replaces the answers sent " + prevAt.UTC().Format(sentAtFormat)
	return strings.Join(append([]string{lines[0], re}, lines[1:]...), "\n"), true
}

// DeckFile is one deck under decisions/ with what the session and the
// viewer wrote for its current round under answers/.
type DeckFile struct {
	Name  string // file name
	Deck  Deck
	Err   error
	Sent  bool      // answers/<id>-r<round>.txt exists
	State DeckState // the draft, empty when there is none
	Ack   string    // answers/<id>-r<round>.ack, first line
}

// Title is the deck title, or the file name when it has none.
func (f DeckFile) Title() string {
	if f.Deck.Title != "" {
		return f.Deck.Title
	}
	return f.Name
}

// AnswerBase is answers/<id>-r<round> in dir, without an extension.
func AnswerBase(dir, id string, round int) string {
	return filepath.Join(dir, "answers", fmt.Sprintf("%s-r%d", id, round))
}

var deckIDRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

func loadDecks(dir string, note func(string)) []DeckFile {
	ents, err := os.ReadDir(filepath.Join(dir, "decisions"))
	if err != nil {
		return nil
	}
	var out []DeckFile
	for _, e := range ents {
		if e.IsDir() || strings.HasPrefix(e.Name(), ".") || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		p := filepath.Join(dir, "decisions", e.Name())
		note(p)
		f := DeckFile{Name: e.Name(), State: newDeckState()}
		b, err := os.ReadFile(p)
		if err == nil {
			err = json.Unmarshal(b, &f.Deck)
		}
		if err == nil && !deckIDRe.MatchString(f.Deck.ID) {
			err = fmt.Errorf("id %q: want letters, digits, ., _ or -", f.Deck.ID)
		}
		if err != nil {
			f.Err = fmt.Errorf("decisions/%s: %w", e.Name(), err)
			out = append(out, f)
			continue
		}
		if f.Deck.Round < 1 {
			f.Deck.Round = 1
		}
		base := AnswerBase(dir, f.Deck.ID, f.Deck.Round)
		if _, err := os.Stat(base + ".txt"); err == nil {
			f.Sent = true
			note(base + ".txt")
		}
		if b, err := os.ReadFile(base + ".ack"); err == nil {
			note(base + ".ack")
			first, _, _ := strings.Cut(strings.TrimSpace(string(b)), "\n")
			f.Ack = strings.TrimSpace(first)
		}
		f.State = LoadDraft(base + ".draft.json")
		out = append(out, f)
	}
	return out
}

// LoadDraft reads a draft file; a missing or broken one is an empty state.
func LoadDraft(path string) DeckState {
	s := newDeckState()
	if b, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(b, &s)
	}
	if s.Choices == nil {
		s.Choices = map[int]string{}
	}
	if s.Notes == nil {
		s.Notes = map[int]string{}
	}
	return s
}

// SaveDraft replaces the draft file atomically.
func SaveDraft(path string, s DeckState) error {
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return writeAtomic(path, append(b, '\n'))
}

// writeAtomic writes data to a temp file next to path and renames it over
// path, creating the directory when missing.
func writeAtomic(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name()) // no-op after the rename
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Chmod(f.Name(), 0o644); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}

// SendAnswers writes the answers of d for its round into
// dir/answers/<id>-r<round>.txt and returns the text written. A round
// already sent with other answers gets the `Re-sent:` line, dated by the
// earlier file's mtime; the same answers again leave the file untouched.
func SendAnswers(dir string, d *Deck, s DeckState) (string, error) {
	p := AnswerBase(dir, d.ID, d.Round) + ".txt"
	var prev string
	var prevAt time.Time
	sent := false
	if b, err := os.ReadFile(p); err == nil {
		prev, sent = string(b), true
		if fi, err := os.Stat(p); err == nil {
			prevAt = fi.ModTime()
		}
	}
	text, changed := Resend(Serialize(d, s), prev, prevAt, sent)
	if !changed {
		return text, nil
	}
	return text, writeAtomic(p, []byte(text))
}

var (
	tagBreakRe = regexp.MustCompile(`(?i)<\s*(br\s*/?|/p|/div|/li|/h[1-6]|/tr|/ul|/ol|/pre|/blockquote)\s*>`)
	tagLiRe    = regexp.MustCompile(`(?i)<\s*li\b[^>]*>`)
	tagRe      = regexp.MustCompile(`<[^>]*>`)
	spaceRe    = regexp.MustCompile(`[ \t\r\f\v]+`)
)

// PlainText turns a deck's HTML field into plain text: block ends become
// line breaks, list items `• `, every other tag is dropped and entities are
// decoded. Blank lines collapse.
func PlainText(h string) string {
	h = tagBreakRe.ReplaceAllString(h, "\n")
	h = tagLiRe.ReplaceAllString(h, "• ")
	h = html.UnescapeString(tagRe.ReplaceAllString(h, ""))
	var out []string
	for _, l := range strings.Split(h, "\n") {
		if l = strings.TrimSpace(spaceRe.ReplaceAllString(l, " ")); l != "" {
			out = append(out, l)
		}
	}
	return strings.Join(out, "\n")
}

// DeckRound is the round of the deck with this id in dir/decisions (its
// file <id>.json, or any deck whose id matches), or 1 when there is none.
func DeckRound(dir, id string) int {
	for _, d := range loadDecks(dir, func(string) {}) {
		if d.Err == nil && (d.Deck.ID == id || d.Name == id+".json") {
			return d.Deck.Round
		}
	}
	return 1
}
