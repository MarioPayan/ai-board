package board

import (
	"fmt"
	"maps"
	"os/exec"
	"runtime"
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// deckView is the open deck: the deck as last read, the answers being
// drafted (autosaved), the focused item and the note being edited.
type deckView struct {
	file    DeckFile
	state   DeckState
	cur     int
	editing int // the item whose note is open (len(open) is the general note), -1 when none
	ta      textarea.Model
	confirm bool   // the footer asks `N open: send anyway? y/n`
	status  string // the footer's first line, until the next key
	body    deckBody
}

// sendW is the width of the Send button at the start of the last line,
// ` Send ` reversed in color, `[Send]` without.
const sendW = 6

// copyText puts text on the clipboard: OSC 52 to the terminal and, when on
// PATH, wl-copy. Tests replace it.
var copyText = func(text string) {
	termenv.Copy(text)
	if p, err := exec.LookPath("wl-copy"); err == nil {
		c := exec.Command(p)
		c.Stdin = strings.NewReader(text)
		_ = c.Run()
	}
}

// openURL opens url in the browser without waiting. Tests replace it.
var openURL = func(url string) error {
	name := "xdg-open"
	if runtime.GOOS == "darwin" {
		name = "open"
	}
	c := exec.Command(name, url)
	if err := c.Start(); err != nil {
		return err
	}
	go func() { _ = c.Wait() }()
	return nil
}

// validDecks are the decks that parsed.
func validDecks(s *Snapshot) []DeckFile {
	var out []DeckFile
	for _, d := range s.Decks {
		if d.Err == nil {
			out = append(out, d)
		}
	}
	return out
}

func (m *model) openDeck(f DeckFile) tea.Cmd {
	ta := textarea.New()
	ta.ShowLineNumbers = false
	ta.SetHeight(3)
	ta.Placeholder = "Type a note…"
	ta.KeyMap.InsertNewline.SetKeys("alt+enter")
	m.dv = &deckView{file: f, state: LoadDraft(m.draftPath(f)), editing: -1, ta: ta}
	m.picking = false
	m.vp.GotoTop()
	m.layout()
	return tea.EnableMouseCellMotion
}

func (m *model) closeDeck() tea.Cmd {
	m.dv, m.picking = nil, false
	m.vp.GotoTop()
	m.layout()
	return tea.DisableMouse
}

func (m *model) draftPath(f DeckFile) string {
	return AnswerBase(m.dir, f.Deck.ID, f.Deck.Round) + ".draft.json"
}

// pressD opens the deck view: the only deck directly, a picker when there
// are several, nothing when there is none.
func (m *model) pressD() tea.Cmd {
	decks := validDecks(m.snap)
	switch len(decks) {
	case 0:
		return nil
	case 1:
		return m.openDeck(decks[0])
	}
	m.picking, m.pickSel = true, 0
	for i, d := range decks { // start on the first deck still waiting for answers
		if !d.Sent {
			m.pickSel = i
			break
		}
	}
	m.vp.GotoTop()
	m.layout()
	return tea.EnableMouseCellMotion
}

// refreshDeck takes the reloaded deck file into the open view. A new round
// starts from that round's draft.
func (m *model) refreshDeck() {
	if m.dv == nil {
		return
	}
	for _, d := range m.snap.Decks {
		if d.Name != m.dv.file.Name || d.Err != nil {
			continue
		}
		if d.Deck.ID != m.dv.file.Deck.ID || d.Deck.Round != m.dv.file.Deck.Round {
			m.dv.state = d.State
			m.dv.status = fmt.Sprintf("round %d", d.Deck.Round)
		}
		m.dv.file = d
		m.dv.cur = min(m.dv.cur, len(d.Deck.OpenItems()))
		return
	}
}

func (dv *deckView) open() []DeckItem { return dv.file.Deck.OpenItems() }

// footLines is the height of the deck footer: two lines, six while a note
// is open.
func (dv *deckView) footLines() int {
	if dv.editing >= 0 {
		return 6
	}
	return 2
}

func (m *model) deckLayout() {
	dv := m.dv
	dv.body = m.r.renderDeck(dv.file, dv.state, dv.cur, m.w)
	m.vp.Height = max(1, m.h-lipgloss.Height(m.top)-dv.footLines())
	m.vp.SetContent(strings.Join(dv.body.lines, "\n"))
	dv.ta.SetWidth(max(10, m.w-2))
}

// saveDraft writes the draft and hands it to the snapshot, so the header
// slot counts the picks before the watcher reloads.
func (m *model) saveDraft() {
	if err := SaveDraft(m.draftPath(m.dv.file), m.dv.state); err != nil {
		m.dv.status = m.r.st.err.Render("draft not saved: " + err.Error())
	}
	for i := range m.snap.Decks {
		if m.snap.Decks[i].Name == m.dv.file.Name {
			m.snap.Decks[i].State = DeckState{Choices: maps.Clone(m.dv.state.Choices), Notes: maps.Clone(m.dv.state.Notes), General: m.dv.state.General}
		}
	}
}

func (m *model) focusItem(i int) {
	dv := m.dv
	dv.cur = max(0, min(i, len(dv.open())))
	m.layout()
	top := dv.body.itemLine[dv.cur]
	if top < m.vp.YOffset || top > m.vp.YOffset+m.vp.Height-4 {
		m.vp.SetYOffset(top)
	}
}

// pick sets option j of open item i, or clears it when it is the pick.
func (m *model) pick(i, j int) {
	open := m.dv.open()
	if i >= len(open) || j < 0 || j >= len(open[i].OptionsList()) {
		return
	}
	n, key := open[i].N, open[i].OptionsList()[j].Key
	if m.dv.state.Choices[n] == key {
		delete(m.dv.state.Choices, n)
	} else {
		m.dv.state.Choices[n] = key
	}
	m.dv.cur = i
	m.saveDraft()
	m.layout()
}

func (m *model) editNote(i int) tea.Cmd {
	dv := m.dv
	dv.cur, dv.editing = i, i
	if i < len(dv.open()) {
		dv.ta.SetValue(dv.state.Notes[dv.open()[i].N])
	} else {
		dv.ta.SetValue(dv.state.General)
	}
	dv.ta.CursorEnd()
	m.layout()
	return dv.ta.Focus()
}

func (m *model) saveNote() {
	dv := m.dv
	v := strings.TrimSpace(dv.ta.Value())
	if dv.editing < len(dv.open()) {
		n := dv.open()[dv.editing].N
		if v == "" {
			delete(dv.state.Notes, n)
		} else {
			dv.state.Notes[n] = v
		}
	} else {
		dv.state.General = v
	}
	m.saveDraft()
}

// send writes the answers file, copies the text and says so in the footer.
func (m *model) send() {
	dv := m.dv
	text, err := SendAnswers(m.dir, &dv.file.Deck, dv.state)
	if err != nil {
		dv.status = m.r.st.err.Render("send failed: " + err.Error())
		return
	}
	copyText(text)
	done, total := dv.file.Deck.Answered(dv.state)
	dv.status = m.r.st.ok.Render(fmt.Sprintf("✓ sent %d/%d · copied", done, total))
	m.reload()
	m.refreshDeck()
	m.layout()
}

// trySend sends, or asks first when items are still open.
func (m *model) trySend() {
	if done, total := m.dv.file.Deck.Answered(m.dv.state); done < total {
		m.dv.confirm = true
		return
	}
	m.send()
}

// nextOpen focuses the next item without a pick after the focused one.
func (m *model) nextOpen() {
	open := m.dv.open()
	for k := 1; k <= len(open); k++ {
		i := (m.dv.cur + k) % len(open)
		if _, ok := open[i].option(m.dv.state.Choices[open[i].N]); !ok {
			m.focusItem(i)
			return
		}
	}
	m.dv.status = m.r.st.dim.Render("every item has a pick")
}

func (m *model) openFocusedURL() {
	dv := m.dv
	u := dv.file.Deck.URL
	if open := dv.open(); dv.cur < len(open) && open[dv.cur].URL != "" {
		u = open[dv.cur].URL
	}
	if u == "" {
		dv.status = m.r.st.dim.Render("no url for this item or deck")
		return
	}
	if err := openURL(u); err != nil {
		dv.status = m.r.st.err.Render("open failed: " + err.Error())
		return
	}
	dv.status = m.r.st.dim.Render("opened " + u)
}

// pickKey maps a-d and 1-9 to an option index, and on a confirm item y, r
// and f to Approve, Reject and Defer.
func pickKey(k string, it DeckItem) (int, bool) {
	if len(k) != 1 {
		return 0, false
	}
	c := k[0]
	switch {
	case c >= 'a' && c <= 'd':
		return int(c - 'a'), true
	case c >= '1' && c <= '9':
		return int(c - '1'), true
	}
	if it.Kind == "confirm" {
		for key, alias := range confirmAlias {
			if alias == k {
				return int(key[0] - 'A'), true
			}
		}
	}
	return 0, false
}

func (m *model) deckKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	dv := m.dv
	if dv.editing >= 0 {
		switch msg.String() {
		case "enter":
			m.saveNote()
			fallthrough
		case "esc":
			dv.editing = -1
			dv.ta.Blur()
			m.layout()
			return m, nil
		}
		var cmd tea.Cmd
		dv.ta, cmd = dv.ta.Update(msg)
		return m, cmd
	}
	if dv.confirm {
		dv.confirm = false
		if msg.String() == "y" {
			m.send()
		}
		return m, nil
	}
	dv.status = ""
	open := dv.open()
	k := msg.String()
	switch k {
	case "ctrl+c", "q":
		return m, tea.Quit
	case "esc", "D":
		return m, m.closeDeck()
	case "j", "down", "tab":
		m.focusItem(dv.cur + 1)
	case "k", "up", "shift+tab":
		m.focusItem(dv.cur - 1)
	case "x":
		if dv.cur < len(open) {
			delete(dv.state.Choices, open[dv.cur].N)
			m.saveDraft()
			m.layout()
		}
	case "n":
		return m, m.editNote(dv.cur)
	case "N":
		return m, m.editNote(len(open))
	case "R":
		n := dv.file.Deck.FillRecommended(dv.state)
		m.saveDraft()
		dv.status = m.r.st.dim.Render(fmt.Sprintf("%d filled with the recommendation", n))
		m.layout()
	case "u":
		m.nextOpen()
	case "o":
		m.openFocusedURL()
	case "s":
		m.trySend()
	default:
		if dv.cur < len(open) {
			if j, ok := pickKey(k, open[dv.cur]); ok {
				m.pick(dv.cur, j)
				return m, nil
			}
		}
		if k != "pgup" && k != "pgdown" {
			return m, nil // the viewport's letter keys (b, d, f, u, space) would scroll
		}
		var cmd tea.Cmd
		m.vp, cmd = m.vp.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m *model) deckMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	dv := m.dv
	if msg.Action != tea.MouseActionPress || msg.Button != tea.MouseButtonLeft {
		var cmd tea.Cmd
		m.vp, cmd = m.vp.Update(msg) // wheel
		return m, cmd
	}
	if dv.editing >= 0 {
		return m, nil
	}
	if dv.confirm {
		dv.confirm = false
	}
	if msg.Y == m.h-1 && msg.X < sendW {
		m.trySend()
		return m, nil
	}
	y := msg.Y - lipgloss.Height(m.top)
	if y < 0 || y >= m.vp.Height {
		return m, nil
	}
	t, ok := dv.body.targets[m.vp.YOffset+y]
	if !ok {
		return m, nil
	}
	dv.status = ""
	switch t.opt {
	case tgtHeader:
		dv.cur = t.item
		m.layout()
	case tgtNote:
		return m, m.editNote(t.item)
	case tgtURL:
		dv.cur = t.item
		m.openFocusedURL()
		m.layout()
	default:
		m.pick(t.item, t.opt)
	}
	return m, nil
}

const deckKeys = "  j/k item  a-d pick  x clear  n note  N general  R rec  u open  o url  esc back"

// deckFooter is two lines: the status (or the send prompt), then the Send
// button and the keys; six while a note is open: a rule, the note's name,
// the text box and its keys.
func (m *model) deckFooter() string {
	dv := m.dv
	st := m.r.st
	if dv.editing >= 0 {
		name := "General notes"
		if open := dv.open(); dv.editing < len(open) {
			name = fmt.Sprintf("Note on %d. %s", open[dv.editing].N, open[dv.editing].Title)
		}
		return st.rule.Render(strings.Repeat("─", m.w)) + "\n" + st.bold.Render(trunc(name, m.w)) + "\n" +
			dv.ta.View() + "\n" + st.dim.Render(trunc("enter save · alt+enter newline · esc cancel", m.w))
	}
	done, total := dv.file.Deck.Answered(dv.state)
	status := dv.status
	switch {
	case dv.confirm:
		status = st.noteWaiting.Render(fmt.Sprintf("%d open: send anyway? y/n", total-done))
	case status == "":
		status = st.dim.Render("click an option to pick it, ✎ to write a note")
	}
	send := " Send "
	if m.r.Color {
		send = st.bold.Reverse(true).Render(send)
	} else {
		send = "[Send]"
	}
	return trunc(status, m.w) + "\n" + trunc(send+st.dim.Render(deckKeys), m.w)
}

func (m *model) pickerKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	decks := validDecks(m.snap)
	switch msg.String() {
	case "ctrl+c", "q":
		return m, tea.Quit
	case "esc", "D":
		return m, m.closeDeck()
	case "j", "down", "tab":
		m.pickSel = min(m.pickSel+1, len(decks)-1)
	case "k", "up", "shift+tab":
		m.pickSel = max(m.pickSel-1, 0)
	case "enter":
		if m.pickSel < len(decks) {
			return m, m.openDeck(decks[m.pickSel])
		}
	}
	m.layout()
	return m, nil
}

func (m *model) pickerMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if msg.Action != tea.MouseActionPress || msg.Button != tea.MouseButtonLeft {
		return m, nil
	}
	decks := validDecks(m.snap)
	// row 0 is the heading; deck i is on row i+1
	if i := msg.Y - lipgloss.Height(m.top) + m.vp.YOffset - 1; i >= 0 && i < len(decks) {
		return m, m.openDeck(decks[i])
	}
	return m, nil
}
