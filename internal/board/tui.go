package board

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/fsnotify/fsnotify"
)

const debounce = 150 * time.Millisecond

type tickMsg time.Time
type fileMsg struct{}
type probeMsg map[string]bool // running url → up
type probeTickMsg struct{}

// probeEvery is how often the live viewer probes the running urls.
const probeEvery = 30 * time.Second

type model struct {
	dir, name string
	r         *Renderer
	snap      *Snapshot
	vp        viewport.Model
	w, h      int
	showLog   bool
	showHelp  bool
	narrow    bool // w: the footer shows keys only
	anchors   []int
	top       string

	dv      *deckView // the open deck, nil on the board
	picking bool      // D with several decks: the deck list is open
	pickSel int

	dismissed map[string]bool // deck rounds closed by hand: not opened again by themselves
	title     string          // the terminal title last set

	watcher *fsnotify.Watcher
	watched map[string]bool
	events  chan struct{}
}

// Run starts the live viewer on dir. name is shown in the terminal title.
func Run(dir, name string) error {
	m := &model{
		dir:     dir,
		name:    name,
		r:       &Renderer{Color: true, Dark: lipgloss.HasDarkBackground()},
		watched: map[string]bool{},
		events:  make(chan struct{}, 1),
	}
	if w, err := fsnotify.NewWatcher(); err == nil {
		m.watcher = w
		defer w.Close()
		go func() {
			// Writers may not rename atomically: wait until events have been
			// quiet for debounce before re-reading.
			t := time.NewTimer(time.Hour)
			t.Stop()
			for {
				select {
				case _, ok := <-w.Events:
					if !ok {
						return
					}
					t.Reset(debounce)
				case <-t.C:
					select {
					case m.events <- struct{}{}:
					default:
					}
				case _, ok := <-w.Errors:
					if !ok {
						return
					}
				}
			}
		}()
	}
	m.vp = viewport.New(80, 20)
	m.vp.KeyMap.Left = key.NewBinding(key.WithDisabled())
	m.vp.KeyMap.Right = key.NewBinding(key.WithDisabled())
	m.reload()
	_, err := tea.NewProgram(m, tea.WithAltScreen()).Run()
	return err
}

func (m *model) watch() {
	if m.watcher == nil {
		return
	}
	for _, p := range []string{m.dir, filepath.Join(m.dir, "sections"), filepath.Join(m.dir, "decisions"), filepath.Join(m.dir, "answers")} {
		if m.watched[p] {
			if _, err := os.Stat(p); err != nil { // removed: re-add when it returns
				delete(m.watched, p)
			}
			continue
		}
		if m.watcher.Add(p) == nil {
			m.watched[p] = true
		}
	}
}

func (m *model) reload() {
	m.watch()
	m.snap = Load(m.dir)
}

func tick() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func (m *model) waitFile() tea.Cmd {
	return func() tea.Msg { <-m.events; return fileMsg{} }
}

// probe probes the running urls off the UI goroutine; the urls are read
// now, so the command never touches the model.
func (m *model) probe() tea.Cmd {
	rs := m.snap.Board.Running
	return func() tea.Msg { return probeMsg(ProbeAll(rs)) }
}

func (m *model) Init() tea.Cmd {
	return tea.Batch(tick(), m.waitFile(), m.probe(), m.syncDeck())
}

func (m *model) layout() {
	if m.w == 0 {
		return
	}
	now := time.Now()
	f := m.r.Render(m.snap, m.w, now)
	m.top, m.anchors = f.Top, f.Anchors
	m.vp.Width = m.w
	if m.dv != nil {
		m.anchors = nil
		m.deckLayout(f.Body)
		return
	}
	body := f.Body
	switch {
	case m.picking:
		body, m.anchors = strings.Join(m.r.renderPicker(validDecks(m.snap), m.pickSel, m.w), "\n"), nil
	case m.showHelp:
		body, m.anchors = m.r.RenderHelp(m.w), nil
	case m.showLog:
		body, m.anchors = m.r.RenderLog(m.snap, m.w), nil
	}
	m.vp.Height = max(1, m.h-lipgloss.Height(m.top)-1-m.legendLines()) // footer and legend lines
	m.vp.SetContent(body)
}

// legendLines is 1 while `g` shows the legend line, otherwise 0.
func (m *model) legendLines() int {
	if m.r.Legend {
		return 1
	}
	return 0
}

// maxBurst is the longest multi-rune key message replayed as keys.
const maxBurst = 4

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
		m.layout()
		return m, nil
	case tickMsg:
		if m.snap.Missing {
			m.reload() // directory may appear; no watch on it yet
		}
		m.layout()
		return m, tick()
	case fileMsg:
		m.reload()
		m.refreshDeck()
		m.layout()
		return m, tea.Batch(m.waitFile(), m.syncDeck())
	case probeMsg:
		m.r.Up = msg
		m.layout()
		return m, tea.Tick(probeEvery, func(time.Time) tea.Msg { return probeTickMsg{} })
	case probeTickMsg:
		return m, m.probe()
	case suggestMsg:
		m.applySuggestion(msg)
		m.layout()
		return m, nil
	case tea.MouseMsg:
		switch {
		case m.dv != nil:
			return m.deckMouse(msg)
		case m.picking:
			return m.pickerMouse(msg)
		}
	case tea.KeyMsg:
		// keys sent in one burst (tmux send-keys, key repeat) arrive as one
		// multi-rune message such as "jO", which matches no key: replay it
		// one rune at a time. Only short bursts: a longer message is likely
		// pasted text (a terminal without bracketed paste), and replaying it
		// could quit on a 'q' or pick options, so it matches no key and is
		// ignored. A paste, and anything typed into an open text box, stays
		// whole.
		if msg.Type == tea.KeyRunes && len(msg.Runes) > 1 && len(msg.Runes) <= maxBurst && !msg.Paste && !msg.Alt && (m.dv == nil || m.dv.editing < 0) {
			var cmds []tea.Cmd
			for _, r := range msg.Runes {
				_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
				cmds = append(cmds, cmd)
			}
			return m, tea.Batch(cmds...)
		}
		switch {
		case m.dv != nil:
			return m.deckKey(msg)
		case m.picking:
			return m.pickerKey(msg)
		}
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "D":
			return m, m.pressD()
		case "l":
			m.showLog, m.showHelp = !m.showLog, false
			m.vp.GotoTop()
			m.layout()
			return m, nil
		case "h":
			m.showHelp, m.showLog = !m.showHelp, false
			m.vp.GotoTop()
			m.layout()
			return m, nil
		case "g":
			m.r.Legend = !m.r.Legend
			m.layout()
			return m, nil
		case "w":
			m.narrow = !m.narrow
			return m, nil
		case "d":
			m.r.Expand = !m.r.Expand
			m.layout()
			return m, nil
		case "c":
			m.r.AllChecks = !m.r.AllChecks
			m.layout()
			return m, nil
		case "a":
			m.r.AllAgents = !m.r.AllAgents
			m.layout()
			return m, nil
		case "tab":
			for _, a := range m.anchors {
				if a > m.vp.YOffset {
					m.vp.SetYOffset(a)
					break
				}
			}
			return m, nil
		case "shift+tab":
			for i := len(m.anchors) - 1; i >= 0; i-- {
				if m.anchors[i] < m.vp.YOffset {
					m.vp.SetYOffset(m.anchors[i])
					break
				}
			}
			return m, nil
		}
	}
	if m.dv != nil && m.dv.editing >= 0 { // the text box's cursor blink
		var cmd tea.Cmd
		m.dv.ta, cmd = m.dv.ta.Update(msg)
		return m, cmd
	}
	var cmd tea.Cmd
	m.vp, cmd = m.vp.Update(msg)
	return m, cmd
}

func (m *model) View() string {
	if m.w == 0 {
		return ""
	}
	if strings.TrimSpace(m.vp.View()) == "" && m.snap.Missing {
		return m.top
	}
	if m.dv != nil {
		return m.top + "\n" + m.deckRegion()
	}
	v := m.top + "\n" + m.vp.View() + "\n"
	switch {
	case m.picking:
		return v + m.r.st.dim.Render(trunc("j/k move · enter or click open · esc back", m.w))
	}
	if m.r.Legend {
		v += m.r.LegendLine(m.w) + "\n"
	}
	return v + m.footer()
}

// footKey is one footer key: what to press, what it does, and whether it
// would do anything right now.
type footKey struct {
	key, label string
	active     bool
}

// footGroup is a run of footer keys under a dim name.
type footGroup struct {
	name string
	keys []footKey
}

// footerGroups is every footer key in its fixed place.
func (m *model) footerGroups() []footGroup {
	// tab only scrolls, so it does nothing when the whole body fits.
	scrolls := m.vp.TotalLineCount() > m.vp.Height
	return []footGroup{
		{"", []footKey{{"⇥", "sect", len(m.anchors) > 1 && scrolls}}},
		{"view", []footKey{
			{"d", "done", finishedSteps(m.snap) > collapseAbove},
			{"a", "agt", agentsHidden(m.snap)},
			{"c", "chk", hasPassingCheck(m.snap)},
			{"l", "log", true},
			{"D", "deck", len(validDecks(m.snap)) > 0},
			{"g", "leg", true},
		}},
		{"", []footKey{{"h", "help", true}, {"w", "narrow", true}, {"q", "", true}}},
	}
}

// footer is one line: every key in a fixed place, drawn darker when it
// would do nothing, and on the right edge `↓ N` lines below the viewport,
// `end` at the bottom, or nothing when the whole body fits. With narrow
// (`w`), group names and labels are left out.
func (m *model) footer() string {
	var groups []string
	for _, g := range m.footerGroups() {
		var keys []string
		for _, k := range g.keys {
			t := k.key
			if !m.narrow && k.label != "" {
				t += " " + k.label
			}
			if !k.active {
				t = m.r.st.off.Render(t)
			}
			keys = append(keys, t)
		}
		l := strings.Join(keys, " ")
		if !m.narrow && g.name != "" {
			l = m.r.st.dim.Render(g.name) + " " + l
		}
		groups = append(groups, l)
	}
	var ind string
	if total := m.vp.TotalLineCount(); total > m.vp.Height {
		ind = "end"
		if below := total - m.vp.YOffset - m.vp.Height; below > 0 {
			ind = fmt.Sprintf("↓ %d", below)
		}
	}
	if ind != "" {
		ind = m.r.st.dim.Render(ind)
	}
	return row(strings.Join(groups, m.r.st.dim.Render(" │ ")), ind, m.w)
}

func finishedSteps(s *Snapshot) int {
	n := 0
	for _, st := range s.Board.Steps {
		if finished(st) {
			n++
		}
	}
	return n
}

// hasPassingCheck reports whether `c` would show anything more: a checks
// section with a passing check, which the collapsed view hides.
func hasPassingCheck(s *Snapshot) bool {
	for _, sec := range s.Sections {
		if sec.Checks == nil {
			continue
		}
		for _, c := range sec.Checks.Checks {
			if c.OK {
				return true
			}
		}
	}
	return false
}
