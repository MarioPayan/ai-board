package board

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func TestProbe(t *testing.T) {
	var methods []string
	handler := func(status int, headOK bool) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			methods = append(methods, r.Method)
			if r.Method == http.MethodHead && !headOK {
				w.WriteHeader(http.StatusMethodNotAllowed)
				return
			}
			w.WriteHeader(status)
		}
	}
	up := httptest.NewServer(handler(http.StatusUnauthorized, true)) // a login wall is up
	defer up.Close()
	noHead := httptest.NewServer(handler(http.StatusOK, false))
	defer noHead.Close()
	noHeadDown := httptest.NewServer(handler(http.StatusBadGateway, false))
	defer noHeadDown.Close()
	broken := httptest.NewServer(handler(http.StatusInternalServerError, true))
	defer broken.Close()
	dead := httptest.NewServer(handler(http.StatusOK, true))
	dead.Close() // nothing listens there any more

	cases := []struct {
		url     string
		want    bool
		methods string
	}{
		{up.URL, true, "HEAD"},
		{noHead.URL, true, "HEAD GET"},
		{noHeadDown.URL, false, "HEAD GET"},
		{broken.URL, false, "HEAD"},
		{dead.URL, false, ""},
	}
	for _, c := range cases {
		methods = nil
		if got := Probe(c.url); got != c.want || strings.Join(methods, " ") != c.methods {
			t.Errorf("Probe(%s) = %v via %v, want %v via %s", c.url, got, methods, c.want, c.methods)
		}
	}
	got := ProbeAll([]Running{{Name: "a", URL: up.URL}, {Name: "b", URL: dead.URL}, {Name: "c", URL: "tcp://x"}, {Name: "d"}})
	if len(got) != 2 || !got[up.URL] || got[dead.URL] {
		t.Errorf("ProbeAll = %v, want only the http urls, a up and b down", got)
	}
}

func TestRunningRender(t *testing.T) {
	s := &Snapshot{Board: BoardFile{Running: []Running{
		{Name: "web", URL: "http://localhost:3000/a/very/long/path/that/will/not/fit", Login: "user in 1Password: Foo"},
		{Name: "api", URL: "http://localhost:8080"},
		{Name: "job"},
	}}}
	r := &Renderer{Options: Options{Up: map[string]bool{"http://localhost:3000/a/very/long/path/that/will/not/fit": true, "http://localhost:8080": false}}}
	r.init()
	rows := r.running(s, 60).lines
	want := []string{
		"✅ web  http://localhost:3000/a/ver…  user in 1Password: Foo", // exactly 60 cells
		"❌ api  http://localhost:8080",
		"?  job",
	}
	if strings.Join(rows, "\n") != strings.Join(want, "\n") {
		t.Errorf("got\n%s\nwant\n%s", strings.Join(rows, "\n"), strings.Join(want, "\n"))
	}
	cr := &Renderer{Color: true}
	cr.init()
	if got := cr.running(s, 60).lines[1]; !strings.Contains(got, ansi.SetHyperlink("http://localhost:8080")) {
		t.Errorf("color row has no hyperlink: %q", got)
	}
}

// The live viewer shows ? until a probe answers, then the result.
func TestModelProbe(t *testing.T) {
	m := &model{r: &Renderer{}, snap: &Snapshot{Board: BoardFile{Running: []Running{{Name: "web", URL: "http://x"}}}}, vp: viewport.New(80, 20)}
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 20})
	if v := ansi.Strip(m.View()); !strings.Contains(v, "?  web") {
		t.Fatalf("before a probe:\n%s", v)
	}
	_, cmd := m.Update(probeMsg{"http://x": true})
	if v := ansi.Strip(m.View()); !strings.Contains(v, "✅ web") || cmd == nil {
		t.Errorf("after a probe (and a next one scheduled):\n%s", v)
	}
}

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
