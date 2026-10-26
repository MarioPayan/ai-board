package board

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLoadAgents(t *testing.T) {
	p := filepath.Join(t.TempDir(), "agents.jsonl")
	write(t, p, `{"ts":"2026-10-26T13:00:00-05:00","event":"start","id":"run","name":"runner","expect_min":30}
{"ts":"2026-10-26T13:01:00-05:00","event":"start","id":"ok","name":"done ok"}
{"ts":"2026-10-26T13:02:00-05:00","event":"stop","id":"ok","ok":true}
{"ts":"2026-10-26T13:03:00-05:00","event":"start","id":"bad","name":"breaks"}
{"ts":"2026-10-26T13:04:00-05:00","event":"stop","id":"bad","ok":false}
{"ts":"2026-10-26T13:05:00-05:00","event":"start","id":"gone","name":"dismissed"}
{"ts":"2026-10-26T13:06:00-05:00","event":"stop","id":"gone","ok":false}
{"ts":"2026-10-26T13:07:00-05:00","event":"dismiss","id":"gone"}
{"ts":"2026-10-26T13:08:00-05:00","event":"start","id":"again","name":"first"}
{"ts":"2026-10-26T13:09:00-05:00","event":"stop","id":"again","ok":false}
{"ts":"2026-10-26T13:10:00-05:00","event":"start","id":"again","name":"retried"}
{"ts":"2026-10-26T13:11:00-05:00","event":"stop","id":"plain"}
garbage
`)
	agents, errs := loadAgents(p)
	got := map[string]Agent{}
	for _, a := range agents {
		got[a.ID] = a
	}
	if len(agents) != 3 {
		t.Fatalf("want 3 agents (run, bad, again), got %+v", agents)
	}
	if a := got["run"]; a.Failed || a.ExpectMin != 30 || a.Name != "runner" {
		t.Errorf("run: %+v", a)
	}
	if a := got["bad"]; !a.Failed || a.Stopped.Format("15:04") != "13:04" {
		t.Errorf("bad should be failed at 13:04: %+v", a)
	}
	if a := got["again"]; a.Failed || a.Name != "retried" {
		t.Errorf("again should be running after restart: %+v", a)
	}
	if len(errs) != 1 || !strings.Contains(errs[0], "agents.jsonl:13") {
		t.Errorf("want one error on line 13, got %v", errs)
	}
}

func TestParseCommit(t *testing.T) {
	cases := []struct{ out, branch, sha, subject string }{
		{"[feat/tui a3f9c1e] feat: render tables\n 2 files changed", "feat/tui", "a3f9c1e", "feat: render tables"},
		{"[main (root-commit) 1af1a0c] docs: spec\n", "main", "1af1a0c", "docs: spec"},
		{"hook output\n[detached HEAD 0123456789abcdef] fix: x [y]\r\n", "detached HEAD", "0123456789abcdef", "fix: x [y]"},
	}
	for _, c := range cases {
		b, s, sub, ok := ParseCommit(c.out)
		if !ok || b != c.branch || s != c.sha || sub != c.subject {
			t.Errorf("ParseCommit(%q) = %q %q %q %v", c.out, b, s, sub, ok)
		}
	}
	for _, bad := range []string{"", "nothing to commit, working tree clean", "[main] no sha", "x [main a3f9c1e] not at line start"} {
		if _, _, _, ok := ParseCommit(bad); ok {
			t.Errorf("ParseCommit(%q) should fail", bad)
		}
	}
}

func TestSectionTitle(t *testing.T) {
	cases := []struct{ name, body, want string }{
		{"10-findings.md", "plain text", "Findings"},
		{"10-findings.md", "\n# Real title\nbody", "Real title"},
		{"10-findings.md", "## Not h1\n", "Findings"},
		{"20-space-usage.chart.json", "", "Space usage"},
		{"30-candidates.table.json", "", "Candidates"},
		{"50-verification.checks.json", "", "Verification"},
		{"notes.md", "", "Notes"},
		{"05_ñandú.md", "", "Ñandú"},
	}
	for _, c := range cases {
		if got, _ := SectionTitle(c.name, []byte(c.body)); got != c.want {
			t.Errorf("SectionTitle(%q, %q) = %q, want %q", c.name, c.body, got, c.want)
		}
	}
}

func TestStatusPrecedence(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "board.json"), `{"title":"x","status":{"state":"working","text":"from board"}}`)
	boardMod := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
	if err := os.Chtimes(filepath.Join(dir, "board.json"), boardMod, boardMod); err != nil {
		t.Fatal(err)
	}
	sp := filepath.Join(dir, "status.json")

	write(t, sp, `{"state":"waiting","text":"from stop hook","ts":"2026-10-26T12:00:05Z"}`)
	if got := Load(dir).Status.Text; got != "from stop hook" {
		t.Errorf("newer status.json: got %q", got)
	}
	write(t, sp, `{"state":"waiting","text":"from stop hook","ts":"2026-10-26T11:59:00Z"}`)
	if got := Load(dir).Status.Text; got != "from board" {
		t.Errorf("older status.json: got %q", got)
	}
	write(t, sp, `{"state":"waiting",`)
	if got := Load(dir).Status.Text; got != "from board" {
		t.Errorf("broken status.json: got %q", got)
	}
	os.Remove(sp)
	if got := Load(dir).Status.Text; got != "from board" {
		t.Errorf("missing status.json: got %q", got)
	}
}

func readJSONL(t *testing.T, path string) []map[string]any {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var out []map[string]any
	for _, l := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		var m map[string]any
		if err := json.Unmarshal([]byte(l), &m); err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		out = append(out, m)
	}
	return out
}

func TestHook(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, ".claude", "boards", "s1")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	payloads := []string{
		`{"session_id":"s1","hook_event_name":"SubagentStart","agent_id":"ag1","agent_type":"repo-worker"}`,
		`{"session_id":"s1","hook_event_name":"SubagentStop","agent_id":"ag1"}`,
		// object tool_response
		`{"session_id":"s1","hook_event_name":"PostToolUse","tool_name":"Bash","tool_input":{"command":"git add -- a && git commit -m 'feat: x'"},"tool_response":{"stdout":"[feat/tui a3f9c1e] feat: x\n 1 file changed","stderr":""}}`,
		// string tool_response, git -C
		`{"session_id":"s1","hook_event_name":"PostToolUse","tool_name":"Bash","tool_input":{"command":"git -C /r commit -F msg"},"tool_response":"[main b71e0d2] fix: y\n"}`,
		// ignored: not a commit, not Bash, no commit line, unknown event
		`{"session_id":"s1","hook_event_name":"PostToolUse","tool_name":"Bash","tool_input":{"command":"echo '[main abcdef1] fake'"},"tool_response":"[main abcdef1] fake"}`,
		`{"session_id":"s1","hook_event_name":"PostToolUse","tool_name":"Read","tool_input":{"command":"git commit"},"tool_response":"[main abcdef1] fake"}`,
		`{"session_id":"s1","hook_event_name":"PostToolUse","tool_name":"Bash","tool_input":{"command":"git commit"},"tool_response":{"stdout":"nothing to commit"}}`,
		`{"session_id":"s1","hook_event_name":"Stop"}`,
		// errors: bad JSON, no board dir, path traversal
		`{not json`,
		`{"session_id":"other","hook_event_name":"SubagentStart","agent_id":"x","agent_type":"y"}`,
		`{"session_id":"../s1","hook_event_name":"SubagentStart","agent_id":"x","agent_type":"y"}`,
	}
	for _, p := range payloads {
		_ = Hook(strings.NewReader(p))
	}
	agents := readJSONL(t, filepath.Join(dir, "agents.jsonl"))
	if len(agents) != 2 || agents[0]["event"] != "start" || agents[0]["id"] != "ag1" || agents[0]["name"] != "repo-worker" ||
		agents[1]["event"] != "stop" || agents[1]["ok"] != true {
		t.Errorf("agents.jsonl: %v", agents)
	}
	log := readJSONL(t, filepath.Join(dir, "log.jsonl"))
	if len(log) != 2 || log[0]["text"] != "a3f9c1e feat: x" || log[0]["kind"] != "commit" || log[1]["text"] != "b71e0d2 fix: y" {
		t.Errorf("log.jsonl: %v", log)
	}
	if _, err := os.Stat(filepath.Join(home, ".claude", "boards", "other")); !os.IsNotExist(err) {
		t.Error("hook created a board directory for an unknown session")
	}
	ents, _ := os.ReadDir(filepath.Join(home, ".claude", "boards"))
	if len(ents) != 1 {
		t.Errorf("boards root should only hold s1, got %d entries", len(ents))
	}
}
