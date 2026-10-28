package board

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
)

func TestPRsRender(t *testing.T) {
	s := &Snapshot{Board: BoardFile{PRs: []PR{
		{Repo: "kazemu/board", N: 12, Title: "a very long title that cannot fit in a narrow pane at all", URL: "https://gh/12", CI: "fail", Review: "changes"},
		{Repo: "r", N: 3, Title: "short", State: "closed"},
	}}}
	r := &Renderer{}
	r.init()
	rows := r.prs(s, 50).lines
	if len(rows) != 2 || ansi.StringWidth(rows[0]) != 50 || !strings.HasPrefix(rows[0], "kazemu/board#12 a very") ||
		!strings.HasSuffix(rows[0], "…         ❌ 👎 ↗") || strings.Contains(rows[0], "https://gh/12") {
		t.Errorf("rows:\n%s", strings.Join(rows, "\n"))
	}
	// the right columns align: row 2's state word starts where row 1's
	// (empty) state column does, 14 cells from the edge
	if want := "r#3 short" + strings.Repeat(" ", 50-14-9) + "closed"; rows[1] != want {
		t.Errorf("row 2 = %q, want %q", rows[1], want)
	}
	cr := &Renderer{Color: true}
	cr.init()
	if got := cr.prs(s, 50).lines[0]; !strings.Contains(got, ansi.SetHyperlink("https://gh/12")) {
		t.Errorf("color row has no hyperlink: %q", got)
	}
	if strings.Contains(Once(&Snapshot{}, 80, time.Now(), Options{}), "PRs") {
		t.Error("PRs heading without PRs")
	}
}
