package board

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func TestModelDoneToggleAndFooter(t *testing.T) {
	m := &model{r: &Renderer{}, snap: Load(filepath.Join("..", "..", "testdata", "done-heavy")), vp: viewport.New(80, 10)}
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 12})
	view := ansi.Strip(m.View())
	lines := strings.Split(view, "\n")
	if len(lines) != 12 {
		t.Errorf("view is %d lines, want the window's 12:\n%s", len(lines), view)
	}
	foot := lines[len(lines)-1]
	if !strings.HasPrefix(foot, "↓ ") || !strings.HasSuffix(foot, "⇥ sections · l log · d done · q") {
		t.Errorf("footer = %q", foot)
	}
	if !strings.Contains(view, "· last:") {
		t.Errorf("finished steps should start collapsed:\n%s", view)
	}

	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d")})
	view = ansi.Strip(m.View())
	if strings.Contains(view, "· last:") || !strings.Contains(view, "Read SPEC.md and README.md") {
		t.Errorf("d should expand finished steps:\n%s", view)
	}

	m.Update(tea.WindowSizeMsg{Width: 80, Height: 60}) // everything fits: no ↓
	lines = strings.Split(ansi.Strip(m.View()), "\n")
	if foot := lines[len(lines)-1]; foot != "⇥ sections · l log · d done · q" {
		t.Errorf("footer without scroll = %q", foot)
	}
}
