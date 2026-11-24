// Package board loads, renders and writes Claude Code session boards.
// SPEC.md in the repository root is the file-format contract.
package board

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Status is board.json `status` (or status.json).
type Status struct {
	State string `json:"state"`
	Text  string `json:"text"`
	TS    string `json:"ts,omitempty"`
}

// Step is one plan step.
type Step struct {
	Text    string `json:"text"`
	State   string `json:"state"`
	Note    string `json:"note"`
	Started string `json:"started"`
	Ended   string `json:"ended"`
	Link    string `json:"link"` // external steps: opened from the header slot
}

// Pending is one item waiting on Mario.
type Pending struct {
	Text string `json:"text"`
	Link string `json:"link"`
}

// PR is one pull request the task tracks.
type PR struct {
	Repo   string `json:"repo"`
	N      int    `json:"n"`
	Title  string `json:"title"`
	URL    string `json:"url"`
	State  string `json:"state"`  // open, draft, merged, closed
	CI     string `json:"ci"`     // pass, fail, pending or ""
	Review string `json:"review"` // approved, changes, pending or ""
}

// Running is one local service the task started.
type Running struct {
	Name  string `json:"name"`
	URL   string `json:"url"`
	Cmd   string `json:"cmd"`
	Login string `json:"login"` // a hint where the credentials live, never a secret
}

// BoardFile is board.json.
type BoardFile struct {
	Title   string    `json:"title"`
	Status  Status    `json:"status"`
	Steps   []Step    `json:"steps"`
	Pending []Pending `json:"pending"`
	PRs     []PR      `json:"prs"`
	Running []Running `json:"running"`
}

// Agent is the resolved state of one subagent.
type Agent struct {
	ID        string
	Name      string
	Started   time.Time
	Stopped   time.Time
	ExpectMin float64
	Failed    bool
}

// LogEntry is one line of log.jsonl.
type LogEntry struct {
	TS   string `json:"ts"`
	Text string `json:"text"`
	Kind string `json:"kind"`

	t   time.Time
	seq int
}

// Section is one file under sections/.
type Section struct {
	Name   string // file name
	Title  string
	Kind   string // md, chart, table, checks
	Body   []byte // content (md: heading stripped)
	Err    error  // read or parse error, rendered in place
	Chart  *Chart
	Table  *Table
	Checks *Checks
	mod    time.Time
}

// Chart is a *.chart.json section.
type Chart struct {
	Type   string    `json:"type"`
	Unit   string    `json:"unit"`
	Label  string    `json:"label"`
	Value  float64   `json:"value"`
	Max    float64   `json:"max"`
	Values []float64 `json:"values"`
	Series []struct {
		Label string  `json:"label"`
		Value float64 `json:"value"`
	} `json:"series"`
}

// Column is a table column.
type Column struct {
	Name  string `json:"name"`
	Align string `json:"align"`
	Bar   bool   `json:"bar"`
}

// Cell is a table cell: text plus an optional numeric value.
type Cell struct {
	Text string
	V    float64
	HasV bool
}

// UnmarshalJSON accepts a string, a number, or {"v": number, "text": string}.
func (c *Cell) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	switch {
	case len(b) > 0 && b[0] == '"':
		return json.Unmarshal(b, &c.Text)
	case len(b) > 0 && b[0] == '{':
		var o struct {
			V    *float64 `json:"v"`
			Text string   `json:"text"`
		}
		if err := json.Unmarshal(b, &o); err != nil {
			return err
		}
		c.Text = o.Text
		if o.V != nil {
			c.V, c.HasV = *o.V, true
			if c.Text == "" {
				c.Text = fmtNum(c.V)
			}
		}
		return nil
	case string(b) == "null":
		return nil
	default:
		if err := json.Unmarshal(b, &c.V); err != nil {
			return fmt.Errorf("cell %s: want string, number or {v,text}", b)
		}
		c.HasV, c.Text = true, fmtNum(c.V)
		return nil
	}
}

// Table is a *.table.json section.
type Table struct {
	Columns []Column `json:"columns"`
	Rows    [][]Cell `json:"rows"`
}

// Check is one verification result in a *.checks.json section.
type Check struct {
	Name   string `json:"name"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail"`
}

// Checks is a *.checks.json section.
type Checks struct {
	Checks []Check `json:"checks"`
}

// Snapshot is everything read from one board directory.
type Snapshot struct {
	Dir       string
	Missing   bool
	Board     BoardFile
	BoardErr  error
	Status    Status // effective status (board.json or status.json, whichever is newer)
	Agents    []Agent
	AgentErrs []string
	Log       []LogEntry
	LogErrs   []string
	Sections  []Section
	Decks     []DeckFile // decisions/*.json, in file-name order
	Updated   time.Time
}

// Load reads a board directory. It never fails: problems end up in the
// snapshot's *Err fields so the rest of the board still renders.
func Load(dir string) *Snapshot {
	s := &Snapshot{Dir: dir}
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		s.Missing = true
		return s
	}
	note := func(p string) {
		if fi, err := os.Stat(p); err == nil && fi.ModTime().After(s.Updated) {
			s.Updated = fi.ModTime()
		}
	}

	bp := filepath.Join(dir, "board.json")
	var boardMod time.Time
	if b, err := os.ReadFile(bp); err == nil {
		note(bp)
		if fi, err := os.Stat(bp); err == nil {
			boardMod = fi.ModTime()
		}
		if err := json.Unmarshal(b, &s.Board); err != nil {
			s.BoardErr = fmt.Errorf("board.json: %w", err)
		}
	} else if !os.IsNotExist(err) {
		s.BoardErr = err
	}
	sp := filepath.Join(dir, "status.json")
	if _, err := os.Stat(sp); err == nil {
		note(sp)
	}
	s.Status = effectiveStatus(s.Board.Status, boardMod, sp)

	ap := filepath.Join(dir, "agents.jsonl")
	note(ap)
	s.Agents, s.AgentErrs = loadAgents(ap)

	lp := filepath.Join(dir, "log.jsonl")
	note(lp)
	s.Log, s.LogErrs = loadLog(lp)

	s.Sections = loadSections(filepath.Join(dir, "sections"), note)
	s.Decks = loadDecks(dir, note)
	return s
}

// effectiveStatus returns status.json when it parses and its ts is newer than
// board.json's mtime, otherwise board.json's status.
func effectiveStatus(board Status, boardMod time.Time, statusPath string) Status {
	b, err := os.ReadFile(statusPath)
	if err != nil {
		return board
	}
	var st Status
	if json.Unmarshal(b, &st) != nil || st.State == "" {
		return board
	}
	ts, err := time.Parse(time.RFC3339, st.TS)
	if err != nil || !ts.After(boardMod) {
		return board
	}
	return st
}

func readLines(path string, each func(n int, line []byte)) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 4*1024*1024)
	n := 0
	for sc.Scan() {
		n++
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		each(n, line)
	}
}

type agentEvent struct {
	TS        string  `json:"ts"`
	Event     string  `json:"event"`
	ID        string  `json:"id"`
	Name      string  `json:"name"`
	ExpectMin float64 `json:"expect_min"`
	OK        *bool   `json:"ok"`
}

func loadAgents(path string) ([]Agent, []string) {
	var errs []string
	var order []string
	state := map[string]*Agent{}
	readLines(path, func(n int, line []byte) {
		var e agentEvent
		if err := json.Unmarshal(line, &e); err != nil {
			errs = append(errs, fmt.Sprintf("agents.jsonl:%d: %v", n, err))
			return
		}
		if e.ID == "" {
			errs = append(errs, fmt.Sprintf("agents.jsonl:%d: missing id", n))
			return
		}
		ts, _ := time.Parse(time.RFC3339, e.TS)
		switch e.Event {
		case "start":
			name := e.Name
			if name == "" {
				name = e.ID
			}
			if _, ok := state[e.ID]; !ok {
				order = append(order, e.ID)
			}
			state[e.ID] = &Agent{ID: e.ID, Name: name, Started: ts, ExpectMin: e.ExpectMin}
		case "stop":
			a, ok := state[e.ID]
			if !ok {
				return
			}
			if e.OK != nil && !*e.OK {
				a.Failed, a.Stopped = true, ts
			} else {
				delete(state, e.ID)
			}
		case "dismiss":
			delete(state, e.ID)
		default:
			errs = append(errs, fmt.Sprintf("agents.jsonl:%d: unknown event %q", n, e.Event))
		}
	})
	var out []Agent
	for _, id := range order {
		if a, ok := state[id]; ok {
			out = append(out, *a)
			delete(state, id) // an id restarted later appears once
		}
	}
	return out, errs
}

func loadLog(path string) ([]LogEntry, []string) {
	var errs []string
	var out []LogEntry
	readLines(path, func(n int, line []byte) {
		var e LogEntry
		if err := json.Unmarshal(line, &e); err != nil {
			errs = append(errs, fmt.Sprintf("log.jsonl:%d: %v", n, err))
			return
		}
		e.t, _ = time.Parse(time.RFC3339, e.TS)
		e.seq = n
		out = append(out, e)
	})
	sort.SliceStable(out, func(i, j int) bool {
		if !out[i].t.Equal(out[j].t) {
			return out[i].t.After(out[j].t)
		}
		return out[i].seq > out[j].seq
	})
	return out, errs
}

var prefixRe = regexp.MustCompile(`^[0-9]+[-_. ]*`)

// SectionTitle derives a section title from its file name (and, for Markdown,
// its first line). It returns the title and whether the first line was a
// heading that should be dropped from the body.
func SectionTitle(name string, body []byte) (string, bool) {
	if strings.HasSuffix(name, ".md") {
		first, _, _ := bytes.Cut(bytes.TrimLeft(body, "\n\r\t "), []byte("\n"))
		f := strings.TrimSpace(string(first))
		if strings.HasPrefix(f, "# ") {
			return strings.TrimSpace(f[2:]), true
		}
	}
	base := name
	for _, ext := range []string{".chart.json", ".table.json", ".checks.json", ".md"} {
		base = strings.TrimSuffix(base, ext)
	}
	base = prefixRe.ReplaceAllString(base, "")
	base = strings.ReplaceAll(base, "-", " ")
	if base == "" {
		return name, false
	}
	r := []rune(base)
	return strings.ToUpper(string(r[0])) + string(r[1:]), false
}

func sectionKind(name string) string {
	switch {
	case strings.HasPrefix(name, "."):
		return ""
	case strings.HasSuffix(name, ".chart.json"):
		return "chart"
	case strings.HasSuffix(name, ".table.json"):
		return "table"
	case strings.HasSuffix(name, ".checks.json"):
		return "checks"
	case strings.HasSuffix(name, ".md"):
		return "md"
	}
	return ""
}

func loadSections(dir string, note func(string)) []Section {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []Section
	for _, e := range ents { // ReadDir sorts by file name
		kind := sectionKind(e.Name())
		if e.IsDir() || kind == "" {
			continue
		}
		p := filepath.Join(dir, e.Name())
		note(p)
		sec := Section{Name: e.Name(), Kind: kind}
		if fi, err := os.Stat(p); err == nil {
			sec.mod = fi.ModTime()
		}
		b, err := os.ReadFile(p)
		if err != nil {
			sec.Title, _ = SectionTitle(e.Name(), nil)
			sec.Err = err
			out = append(out, sec)
			continue
		}
		var drop bool
		sec.Title, drop = SectionTitle(e.Name(), b)
		switch kind {
		case "md":
			if drop {
				b = bytes.TrimLeft(b, "\n\r\t ")
				if i := bytes.IndexByte(b, '\n'); i >= 0 {
					b = b[i+1:]
				} else {
					b = nil
				}
			}
			sec.Body = b
		case "chart":
			var c Chart
			if err := json.Unmarshal(b, &c); err != nil {
				sec.Err = err
			} else {
				sec.Chart = &c
			}
		case "table":
			var t Table
			if err := json.Unmarshal(b, &t); err != nil {
				sec.Err = err
			} else {
				sec.Table = &t
			}
		case "checks":
			var c Checks
			if err := json.Unmarshal(b, &c); err != nil {
				sec.Err = err
			} else {
				sec.Checks = &c
			}
		}
		out = append(out, sec)
	}
	return out
}
