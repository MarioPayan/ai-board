package board

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

func TestModelDoneToggleAndFooter(t *testing.T) {
	m := &model{r: &Renderer{}, snap: Load(filepath.Join("..", "..", "testdata", "done-heavy")), vp: viewport.New(120, 10)}
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 12})
	view := ansi.Strip(m.View())
	lines := strings.Split(view, "\n")
	if len(lines) != 12 {
		t.Errorf("view is %d lines, want the window's 12:\n%s", len(lines), view)
	}
	keys, ind := footerKeys(m)
	if !strings.HasPrefix(ind, "↓ ") || keys != fullFooter || !m.footerGroups()[0].keys[0].active || !m.footerGroups()[1].keys[0].active {
		t.Errorf("footer = %q %q, want every key, ⇥ and d active, and ↓ N", keys, ind)
	}
	if !strings.Contains(view, "· last:") {
		t.Errorf("finished steps should start collapsed:\n%s", view)
	}

	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d")})
	view = ansi.Strip(m.View())
	if strings.Contains(view, "· last:") || !strings.Contains(view, "Read SPEC.md and README.md") {
		t.Errorf("d should expand finished steps:\n%s", view)
	}

	m.Update(tea.WindowSizeMsg{Width: 120, Height: 60}) // everything fits: no ↓ and ⇥ idle (tab would not scroll)
	keys, ind = footerKeys(m)
	if ind != "" || keys != fullFooter || m.footerGroups()[0].keys[0].active || !m.footerGroups()[1].keys[0].active {
		t.Errorf("footer without scroll = %q %q, want every key, ⇥ idle, d active, no indicator", keys, ind)
	}
}

func TestModelChecksToggle(t *testing.T) {
	m := &model{r: &Renderer{}, snap: Load(filepath.Join("..", "..", "testdata", "checks-fail")), vp: viewport.New(80, 10)}
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 40})
	view := ansi.Strip(m.View())
	if !strings.Contains(view, "❌ 2/5 checks failed") || strings.Contains(view, "gofmt") {
		t.Errorf("checks should start with only the failing ones:\n%s", view)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("c")})
	if view = ansi.Strip(m.View()); !strings.Contains(view, "✅ gofmt -l . — no files") {
		t.Errorf("c should list passing checks:\n%s", view)
	}
}

// a lists every agent in the scrolling body; the fixed area keeps its six
// lines and its top three items whatever the number of agents.
func TestModelAgentsToggle(t *testing.T) {
	m := &model{r: &Renderer{}, snap: Load(filepath.Join("..", "..", "testdata", "agents-many")), vp: viewport.New(80, 10)}
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 14})
	collapsed := ansi.Strip(m.top)
	if !strings.HasSuffix(strings.Split(collapsed, "\n")[4], " +6") || lipgloss.Height(m.top) != 6 {
		t.Errorf("collapsed: want 3 items and +6 (8 agents, 1 external step) in a 6-line fixed area:\n%s", collapsed)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
	if top := ansi.Strip(m.top); top != collapsed {
		t.Errorf("expanded: the fixed area should not change:\n%s", top)
	}
	view := ansi.Strip(m.View())
	if !strings.Contains(view, "🤖 Agents") || lipgloss.Height(m.View()) != 14 {
		t.Errorf("expanded: agents heading in the body, view still 14 lines:\n%s", view)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
	if strings.Contains(ansi.Strip(m.View()), "🤖 Agents") {
		t.Errorf("a again should take the list out of the body:\n%s", ansi.Strip(m.View()))
	}
}

// The board view replays a multi-rune burst one key at a time too.
func TestBoardKeyBurst(t *testing.T) {
	m := &model{r: &Renderer{}, snap: Load(filepath.Join("..", "..", "testdata", "done-heavy")), vp: viewport.New(120, 10)}
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 12})
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("dg")})
	if !m.r.Expand || !m.r.Legend {
		t.Errorf(`"dg" should expand finished steps and show the legend: expand %v legend %v`, m.r.Expand, m.r.Legend)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("dg"), Paste: true}) // a paste is not keys
	if !m.r.Expand || !m.r.Legend {
		t.Errorf("a paste toggled keys: expand %v legend %v", m.r.Expand, m.r.Legend)
	}
}

// A long unbracketed run of characters is pasted text, not keys: it must
// not quit on its 'q' nor toggle anything.
func TestBoardLongRunIgnored(t *testing.T) {
	m := &model{r: &Renderer{}, snap: Load(filepath.Join("..", "..", "testdata", "done-heavy")), vp: viewport.New(120, 10)}
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 12})
	paste := []rune("dg see the queue log")
	if len(paste) != 20 {
		t.Fatalf("paste is %d runes", len(paste))
	}
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: paste})
	if cmd != nil {
		if _, quit := cmd().(tea.QuitMsg); quit {
			t.Fatal("a 20-rune unbracketed message quit the board")
		}
	}
	if m.r.Expand || m.r.Legend || m.showLog || m.showHelp {
		t.Errorf("a 20-rune message acted as keys: expand %v legend %v log %v help %v", m.r.Expand, m.r.Legend, m.showLog, m.showHelp)
	}
}
