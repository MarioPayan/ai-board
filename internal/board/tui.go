package board

import (
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

type model struct {
	dir, name string
	r         *Renderer
	snap      *Snapshot
	vp        viewport.Model
	w, h      int
	showLog   bool
	anchors   []int
	top       string

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
	for _, p := range []string{m.dir, filepath.Join(m.dir, "sections")} {
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

func (m *model) Init() tea.Cmd {
	return tea.Batch(tea.SetWindowTitle("board: "+m.name), tick(), m.waitFile())
}

func (m *model) layout() {
	if m.w == 0 {
		return
	}
	now := time.Now()
	f := m.r.Render(m.snap, m.w, now)
	m.top, m.anchors = f.Top, f.Anchors
	body := f.Body
	if m.showLog {
		body = m.r.RenderLog(m.snap, m.w)
		m.anchors = nil
	}
	m.vp.Width = m.w
	m.vp.Height = max(1, m.h-lipgloss.Height(m.top))
	m.vp.SetContent(body)
}

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
		m.layout()
		return m, m.waitFile()
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "l":
			m.showLog = !m.showLog
			m.vp.GotoTop()
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
	return m.top + "\n" + m.vp.View()
}
