package board

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func TestLegendLine(t *testing.T) {
	r := &Renderer{}
	// order follows the legend, not the input; duplicates and unknowns ignored
	got := r.LegendLine([]string{"◆", "✔", "▶", "✔", "?"}, 80)
	if want := "▶ active · ✔ done · ◆ commit"; got != want {
		t.Errorf("LegendLine = %q, want %q", got, want)
	}
	if got := r.LegendLine(nil, 80); got != "" {
		t.Errorf("no symbols: %q", got)
	}
	// entries that do not fit are dropped whole
	got = r.LegendLine([]string{"▶", "🟡", "🟣", "✘", "○", "✔"}, 30)
	if ansi.StringWidth(got) > 30 || !strings.HasPrefix(got, "▶ active · 🟡 on you") || !strings.HasSuffix(got, "…") {
		t.Errorf("narrow legend = %q", got)
	}
}

// The --once legend names exactly the symbols the frame draws.
func TestOnceLegendFromFrame(t *testing.T) {
	s := Load(filepath.Join("..", "..", "testdata", "checks-pass"))
	got := Once(s, 80, s.Updated, Options{Legend: true})
	lines := strings.Split(strings.TrimRight(got, "\n"), "\n")
	legend := lines[len(lines)-1]
	// ▶ active step, ✔ done step and checks, ⏱ elapsed, ↻ updated, 🔵 status
	if want := "▶ active · ✔ done · ⏱ elapsed · ↻ updated · 🔵 working"; legend != want {
		t.Errorf("legend = %q, want %q\n%s", legend, want, got)
	}
	if strings.Contains(Once(s, 80, s.Updated, Options{}), "active ·") {
		t.Error("legend without --legend")
	}
}

func newModel(t *testing.T, fixture string, w, h int) *model {
	t.Helper()
	m := &model{r: &Renderer{}, snap: Load(filepath.Join("..", "..", "testdata", fixture)), vp: viewport.New(w, h)}
	m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	return m
}

func viewLines(m *model) []string {
	return strings.Split(ansi.Strip(m.View()), "\n")
}

// The live legend sits above the footer and names only symbols on screen:
// the log's ◆ appears once it scrolls into view.
func TestModelLegendVisibleOnly(t *testing.T) {
	m := newModel(t, "done-heavy", 80, 12)
	lines := viewLines(m)
	legend := lines[len(lines)-2]
	if !strings.Contains(legend, "▶ active") || !strings.Contains(legend, "⏱ elapsed") || strings.Contains(legend, "◆") {
		t.Errorf("top of done-heavy: legend = %q\n%s", legend, strings.Join(lines, "\n"))
	}
	m.vp.GotoBottom()
	lines = viewLines(m)
	legend = lines[len(lines)-2]
	if !strings.Contains(legend, "◆ commit") || strings.Contains(legend, "▶") {
		t.Errorf("bottom of done-heavy: legend = %q\n%s", legend, strings.Join(lines, "\n"))
	}
}

// The footer leaves out keys that would do nothing.
func TestModelFooterKeys(t *testing.T) {
	cases := []struct{ fixture, want string }{
		{"done-heavy", "⇥ sections · d done · l log · ? help · q"},
		{"agents-many", "a agents · l log · ? help · q"},   // 8 agents, only a plan
		{"agents-demo", "⇥ sections · l log · ? help · q"}, // 3 agents, 2 finished steps
		{"checks-fail", "⇥ sections · c checks · l log · ? help · q"},
	}
	for _, c := range cases {
		m := newModel(t, c.fixture, 80, 12) // short: the body scrolls
		lines := viewLines(m)
		got := lines[len(lines)-1]
		if i := strings.Index(got, " · "); strings.HasPrefix(got, "↓ ") && i >= 0 {
			got = got[i+len(" · "):]
		}
		if got != c.want {
			t.Errorf("%s: footer = %q, want %q", c.fixture, got, c.want)
		}
		m = newModel(t, c.fixture, 80, 200) // tall: everything fits, so tab would not scroll
		lines = viewLines(m)
		if got := lines[len(lines)-1]; strings.Contains(got, "⇥") || strings.HasPrefix(got, "↓") {
			t.Errorf("%s tall: footer = %q, want no ↓ and no ⇥", c.fixture, got)
		}
	}
}

// ? opens a full-screen help naming every marker and key; ? again closes it.
func TestModelHelp(t *testing.T) {
	m := newModel(t, "agents-many", 80, 80)
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("?")})
	view := ansi.Strip(m.View())
	if strings.Contains(view, "📋 Plan") || !strings.Contains(view, "❔ Help") {
		t.Fatalf("? should replace the body with help:\n%s", view)
	}
	for _, it := range legendItems {
		if !strings.Contains(view, it.sym) {
			t.Errorf("help misses %s (%s)", it.sym, it.label)
		}
	}
	for _, k := range []string{"d ", "a ", "c ", "l ", "? ", "q ", "⇥ ", "j k"} {
		if !strings.Contains(view, "\n"+k) {
			t.Errorf("help misses key %q", k)
		}
	}
	for _, c := range []string{"yellow", "red", "magenta", "cyan", "green", "dim"} {
		if !strings.Contains(view, c+":") {
			t.Errorf("help misses colour %q", c)
		}
	}
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("?")})
	if view = ansi.Strip(m.View()); !strings.Contains(view, "📋 Plan") {
		t.Errorf("? again should close help:\n%s", view)
	}
}
