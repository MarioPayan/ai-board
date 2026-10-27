package board

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
)

const fixedLegend = "▶️ active · 📥 on you · ⏳ external · ❌ failed · ⚪ todo · ✅ done"

func TestLegendLine(t *testing.T) {
	r := &Renderer{}
	// the legend is fixed: the plan markers, in order
	if got := r.LegendLine(80); got != fixedLegend {
		t.Errorf("LegendLine = %q, want %q", got, fixedLegend)
	}
	// entries that do not fit are dropped whole
	got := r.LegendLine(30)
	if ansi.StringWidth(got) > 30 || !strings.HasPrefix(got, "▶️ active · 📥 on you") || !strings.HasSuffix(got, "…") {
		t.Errorf("narrow legend = %q", got)
	}
}

// --once --legend ends the frame with the same fixed legend line.
func TestOnceLegendFromFrame(t *testing.T) {
	s := Load(filepath.Join("..", "..", "testdata", "checks-pass"))
	got := Once(s, 80, s.Updated, Options{Legend: true})
	lines := strings.Split(strings.TrimRight(got, "\n"), "\n")
	if legend := lines[len(lines)-1]; legend != fixedLegend || lines[len(lines)-2] != "" {
		t.Errorf("legend = %q, want %q after a blank line\n%s", legend, fixedLegend, got)
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

func press(m *model, k string) { m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}) }

// The legend line is hidden by default; g shows the fixed legend above the
// footer and the viewport gives up a line for it.
func TestModelLegendToggle(t *testing.T) {
	m := newModel(t, "done-heavy", 120, 14)
	h := m.vp.Height
	lines := viewLines(m)
	if len(lines) != 14 || strings.Contains(strings.Join(lines, "\n"), "⚪ todo") {
		t.Errorf("legend should start hidden:\n%s", strings.Join(lines, "\n"))
	}
	press(m, "g")
	lines = viewLines(m)
	if len(lines) != 14 || lines[len(lines)-2] != fixedLegend || m.vp.Height != h-1 {
		t.Errorf("g: want the legend above the footer and a viewport of %d (got %d):\n%s", h-1, m.vp.Height, strings.Join(lines, "\n"))
	}
	m.vp.GotoBottom() // the legend does not depend on what is on screen
	if lines = viewLines(m); lines[len(lines)-2] != fixedLegend {
		t.Errorf("g at the bottom: legend = %q", lines[len(lines)-2])
	}
	press(m, "g")
	if lines = viewLines(m); len(lines) != 14 || m.vp.Height != h || strings.Contains(strings.Join(lines, "\n"), "⚪ todo") {
		t.Errorf("g again should hide the legend:\n%s", strings.Join(lines, "\n"))
	}
}

const fullFooter = "⇥ sect │ view d done a agents c checks l log g legend │ h help w narrow q"

// footerKeys strips the scroll indicator off the right edge of the footer.
func footerKeys(m *model) (keys, indicator string) {
	lines := viewLines(m)
	f := strings.TrimRight(lines[len(lines)-1], " ")
	if i := strings.LastIndex(f, "  "); i >= 0 {
		if tail := strings.TrimSpace(f[i:]); tail == "end" || strings.HasPrefix(tail, "↓ ") {
			return strings.TrimRight(f[:i], " "), tail
		}
	}
	return f, ""
}

// The footer keeps every key in its place; keys that would do nothing are
// drawn darker instead of being left out.
func TestModelFooterKeys(t *testing.T) {
	cases := []struct{ fixture, active string }{
		{"done-heavy", "⇥ d l g h w q"},
		{"agents-many", "a l g h w q"},   // 8 agents, only a plan
		{"agents-demo", "⇥ a l g h w q"}, // 3 agents, but pending and failures push the running ones out of the slots
		{"checks-fail", "⇥ c l g h w q"}, // a passing check to reveal
	}
	for _, c := range cases {
		m := newModel(t, c.fixture, 120, 12) // short: the body scrolls
		if got, _ := footerKeys(m); got != fullFooter {
			t.Errorf("%s: footer = %q, want %q", c.fixture, got, fullFooter)
		}
		var active []string
		for _, g := range m.footerGroups() {
			for _, k := range g.keys {
				if k.active {
					active = append(active, k.key)
				}
			}
		}
		if got := strings.Join(active, " "); got != c.active {
			t.Errorf("%s: active keys %q, want %q", c.fixture, got, c.active)
		}
		m = newModel(t, c.fixture, 120, 200) // tall: everything fits, so tab would not scroll
		if got, ind := footerKeys(m); got != fullFooter || ind != "" || m.footerGroups()[0].keys[0].active {
			t.Errorf("%s tall: footer = %q, indicator %q, want every key, ⇥ idle and no indicator", c.fixture, got, ind)
		}
	}
}

// withColor renders styles as ANSI escapes for the rest of the test.
func withColor(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.ANSI)
	t.Cleanup(func() { lipgloss.SetColorProfile(prev) })
}

// An idle key is drawn in the darker style, an active one is not.
func TestFooterIdleKeysDarker(t *testing.T) {
	withColor(t)
	m := &model{r: &Renderer{Color: true}, snap: Load(filepath.Join("..", "..", "testdata", "agents-many")), vp: viewport.New(120, 12)}
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 12})
	f := m.footer()
	off := m.r.st.off
	if !strings.Contains(f, off.Render("d done")) || !strings.Contains(f, off.Render("c checks")) {
		t.Errorf("idle d and c should be darker: %q", f)
	}
	if strings.Contains(f, off.Render("a agents")) || !strings.Contains(f, "a agents") {
		t.Errorf("active a should not be darker: %q", f)
	}
}

// The right edge says how many lines are below, `end` at the bottom, and
// nothing when the whole body fits.
func TestModelScrollIndicator(t *testing.T) {
	m := newModel(t, "done-heavy", 120, 12)
	_, ind := footerKeys(m)
	if !strings.HasPrefix(ind, "↓ ") || ind == "↓ 0" {
		t.Errorf("top: indicator = %q, want ↓ N", ind)
	}
	m.vp.GotoBottom()
	if _, ind = footerKeys(m); ind != "end" {
		t.Errorf("bottom: indicator = %q, want end", ind)
	}
	m = newModel(t, "done-heavy", 120, 200)
	if _, ind = footerKeys(m); ind != "" {
		t.Errorf("fits: indicator = %q, want none", ind)
	}
}

// w drops group names and labels, keeping each key in the same order.
func TestModelNarrowFooter(t *testing.T) {
	m := newModel(t, "done-heavy", 120, 12)
	press(m, "w")
	lines := viewLines(m)
	f := lines[len(lines)-1]
	if !strings.HasPrefix(f, "⇥ │ d a c l g │ h w q ") || !strings.Contains(f, "↓ ") || ansi.StringWidth(f) != 120 {
		t.Errorf("narrow footer = %q", f)
	}
	press(m, "w")
	if got, _ := footerKeys(m); got != fullFooter {
		t.Errorf("w again: footer = %q", got)
	}
	// still one line when the pane is narrower than the footer
	m = newModel(t, "done-heavy", 40, 12)
	lines = viewLines(m)
	if f := lines[len(lines)-1]; ansi.StringWidth(f) > 40 || len(lines) != 12 {
		t.Errorf("narrow pane: footer = %q (%d lines)", f, len(lines))
	}
}

// h opens a full-screen help naming every marker and key; h again closes it.
func TestModelHelp(t *testing.T) {
	m := newModel(t, "agents-many", 80, 80)
	press(m, "h")
	view := ansi.Strip(m.View())
	if strings.Contains(view, "📋 Plan") || !strings.Contains(view, "❔ Help") {
		t.Fatalf("h should replace the body with help:\n%s", view)
	}
	for _, sym := range []string{"▶️", "🟡", "⏳", "❌", "⚪", "✅", "⏱", "↻", "◆", "•", "↗", "📥", "🤖", "🔴", "🔵", "🟢", "⚠️", "⚪", "+N"} {
		if !strings.Contains(view, sym) {
			t.Errorf("help misses %s", sym)
		}
	}
	for _, k := range []string{"d ", "a ", "c ", "l ", "g ", "h ", "w ", "q ", "⇥ ", "j k"} {
		if !strings.Contains(view, "\n"+k) {
			t.Errorf("help misses key %q", k)
		}
	}
	if strings.Contains(view, "\n? ") {
		t.Error("help still lists ?")
	}
	for _, c := range []string{"yellow", "red", "magenta", "cyan", "green", "dim"} {
		if !strings.Contains(view, c+":") {
			t.Errorf("help misses colour %q", c)
		}
	}
	press(m, "h")
	if view = ansi.Strip(m.View()); !strings.Contains(view, "📋 Plan") {
		t.Errorf("h again should close help:\n%s", view)
	}
}
