package board

import (
	"math"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

var eighths = []string{"", "▏", "▎", "▍", "▌", "▋", "▊", "▉"}

// hbar draws a bar of frac*cells cells with eighth-block resolution and no
// background, padded with spaces to cells.
func hbar(frac float64, cells int) string {
	frac = math.Max(0, math.Min(1, frac))
	n := int(math.Round(frac * float64(cells) * 8))
	s := strings.Repeat("█", n/8) + eighths[n%8]
	return s + strings.Repeat(" ", cells-ansi.StringWidth(s))
}

// meter is a fixed-width bar on a ░ background, for progress and gauges.
func meter(frac float64, cells int) string {
	frac = math.Max(0, math.Min(1, frac))
	full := int(math.Round(frac * float64(cells)))
	return strings.Repeat("█", full) + strings.Repeat("░", cells-full)
}

var sparks = []rune("▁▂▃▄▅▆▇█")

func sparkline(vals []float64, width int) string {
	if len(vals) > width {
		vals = vals[len(vals)-width:]
	}
	lo, hi := math.Inf(1), math.Inf(-1)
	for _, v := range vals {
		lo, hi = math.Min(lo, v), math.Max(hi, v)
	}
	var b strings.Builder
	for _, v := range vals {
		i := 0
		if hi > lo {
			i = int(math.Round((v - lo) / (hi - lo) * 7))
		}
		b.WriteRune(sparks[i])
	}
	return b.String()
}

func pad(s string, w int, right bool) string {
	s = trunc(s, w)
	g := strings.Repeat(" ", max(0, w-ansi.StringWidth(s)))
	if right {
		return g + s
	}
	return s + g
}

func (r *Renderer) chart(c *Chart, width int) []string {
	switch c.Type {
	case "bar":
		if len(c.Series) == 0 {
			return []string{r.st.dim.Render("no data")}
		}
		lw, vw, hi := 0, 0, 0.0
		for _, p := range c.Series {
			lw = max(lw, ansi.StringWidth(p.Label))
			vw = max(vw, ansi.StringWidth(fmtNum(p.Value)+c.Unit))
			hi = math.Max(hi, p.Value)
		}
		lw = min(lw, width*2/5)
		bw := max(4, width-lw-vw-2)
		var lines []string
		for _, p := range c.Series {
			f := 0.0
			if hi > 0 {
				f = p.Value / hi
			}
			lines = append(lines, pad(p.Label, lw, false)+" "+r.st.bar.Render(hbar(f, bw))+" "+pad(fmtNum(p.Value)+c.Unit, vw, true))
		}
		return lines
	case "sparkline":
		if len(c.Values) == 0 {
			return []string{r.st.dim.Render("no data")}
		}
		pre := ""
		if c.Label != "" {
			pre = c.Label + " "
		}
		last := " " + fmtNum(c.Values[len(c.Values)-1]) + c.Unit
		w := width - ansi.StringWidth(pre) - ansi.StringWidth(last)
		return []string{pre + r.st.bar.Render(sparkline(c.Values, max(1, w))) + last}
	case "gauge":
		f := 0.0
		if c.Max > 0 {
			f = c.Value / c.Max
		}
		right := " " + fmtNum(c.Value) + "/" + fmtNum(c.Max) + " " + c.Unit
		right = strings.TrimRight(right, " ") + " " + fmtNum(math.Round(f*100)) + "%"
		pre := ""
		if c.Label != "" {
			pre = c.Label + " "
		}
		bw := max(4, width-ansi.StringWidth(pre)-ansi.StringWidth(right))
		bs := r.st.bar
		if f > 0.9 {
			bs = r.st.barOver
		}
		return []string{pre + bs.Render(meter(f, bw)) + right}
	}
	return []string{r.errLine("unknown chart type "+quote(c.Type), width)}
}

func quote(s string) string { return "\"" + s + "\"" }

func (r *Renderer) table(t *Table, width int) []string {
	nc := len(t.Columns)
	if nc == 0 {
		return []string{r.errLine("table has no columns", width)}
	}
	cell := func(row []Cell, i int) Cell {
		if i < len(row) {
			return row[i]
		}
		return Cell{}
	}
	ws := make([]int, nc)
	colMax := make([]float64, nc)
	for i, c := range t.Columns {
		ws[i] = ansi.StringWidth(c.Name)
		for _, row := range t.Rows {
			cl := cell(row, i)
			ws[i] = max(ws[i], ansi.StringWidth(cl.Text))
			if cl.HasV {
				colMax[i] = math.Max(colMax[i], cl.V)
			}
		}
	}
	const gap = 2
	fixed := gap * (nc - 1)
	bars := 0
	for i, c := range t.Columns {
		fixed += ws[i]
		if c.Bar {
			bars++
			fixed++ // space before the bar
		}
	}
	// bars get 12–24 cells; shrink the widest column (to 12) to make room,
	// then until the table fits
	bw := 0
	if bars > 0 {
		for (width-fixed)/bars < 12 && shrinkWidest(ws, 12) {
			fixed--
		}
		bw = min(24, max(4, (width-fixed)/bars))
		fixed += bw * bars
	}
	for fixed > width && shrinkWidest(ws, 6) {
		fixed--
	}
	line := func(cells func(i int) (string, string)) string {
		var b strings.Builder
		for i, c := range t.Columns {
			if i > 0 {
				b.WriteString(strings.Repeat(" ", gap))
			}
			text, bar := cells(i)
			b.WriteString(pad(text, ws[i], c.Align == "right"))
			if c.Bar {
				b.WriteString(" " + bar)
			}
		}
		return strings.TrimRight(b.String(), " ")
	}
	var lines []string
	lines = append(lines, r.st.bold.Render(line(func(i int) (string, string) {
		return t.Columns[i].Name, strings.Repeat(" ", bw)
	})))
	lines = append(lines, r.st.dim.Render(line(func(i int) (string, string) {
		// nothing under a bar: it has no header above it
		return strings.Repeat("─", ws[i]), strings.Repeat(" ", bw)
	})))
	for _, row := range t.Rows {
		lines = append(lines, line(func(i int) (string, string) {
			cl := cell(row, i)
			bar := strings.Repeat(" ", bw)
			if t.Columns[i].Bar && cl.HasV && colMax[i] > 0 {
				bar = r.st.bar.Render(hbar(cl.V/colMax[i], bw))
			}
			return cl.Text, bar
		}))
	}
	return lines
}

// shrinkWidest narrows the widest column by one cell unless it is at floor.
func shrinkWidest(ws []int, floor int) bool {
	wi := 0
	for i := range ws {
		if ws[i] > ws[wi] {
			wi = i
		}
	}
	if ws[wi] <= floor {
		return false
	}
	ws[wi]--
	return true
}
