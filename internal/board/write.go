package board

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// BoardsRoot is ~/.claude/boards.
func BoardsRoot() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".claude", "boards"), nil
}

// SessionDir resolves a session id to its board directory.
func SessionDir(id string) (string, error) {
	if id == "" || id == "." || id == ".." || strings.ContainsAny(id, `/\`) {
		return "", errors.New("invalid session id " + quote(id))
	}
	root, err := BoardsRoot()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, id), nil
}

// AppendJSON appends v as one JSON line in a single write.
func AppendJSON(path string, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o644)
	if err != nil {
		return err
	}
	if _, err := f.Write(append(b, '\n')); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

func nowTS() string { return time.Now().Format(time.RFC3339) }

// AppendLog appends a log entry to dir/log.jsonl.
func AppendLog(dir, text, kind string) error {
	e := map[string]string{"ts": nowTS(), "text": text}
	if kind != "" {
		e["kind"] = kind
	}
	return AppendJSON(filepath.Join(dir, "log.jsonl"), e)
}

// AgentStart appends a start event to dir/agents.jsonl.
func AgentStart(dir, id, name string, expectMin float64) error {
	e := map[string]any{"ts": nowTS(), "event": "start", "id": id, "name": name}
	if expectMin > 0 {
		e["expect_min"] = expectMin
	}
	return AppendJSON(filepath.Join(dir, "agents.jsonl"), e)
}

// AgentStop appends a stop event to dir/agents.jsonl.
func AgentStop(dir, id string, ok bool) error {
	return AppendJSON(filepath.Join(dir, "agents.jsonl"),
		map[string]any{"ts": nowTS(), "event": "stop", "id": id, "ok": ok})
}

var commitLineRe = regexp.MustCompile(`(?m)^\[(.+?) ([0-9a-f]{7,40})\] (.+?)\r?$`)

// ParseCommit finds git's `[<branch> <sha>] <subject>` line in commit output.
func ParseCommit(out string) (branch, sha, subject string, ok bool) {
	m := commitLineRe.FindStringSubmatch(out)
	if m == nil {
		return "", "", "", false
	}
	branch = strings.TrimSuffix(m[1], " (root-commit)")
	return branch, m[2], strings.TrimSpace(m[3]), true
}

var gitCommitRe = regexp.MustCompile(`\bgit\b[^|;&\n]*\bcommit\b`)

type hookPayload struct {
	SessionID    string          `json:"session_id"`
	Event        string          `json:"hook_event_name"`
	AgentID      string          `json:"agent_id"`
	AgentType    string          `json:"agent_type"`
	ToolName     string          `json:"tool_name"`
	ToolInput    json.RawMessage `json:"tool_input"`
	ToolResponse json.RawMessage `json:"tool_response"`
}

// toolOutput accepts a string or an object with stdout.
func toolOutput(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var o struct {
		Stdout string `json:"stdout"`
	}
	_ = json.Unmarshal(raw, &o)
	return o.Stdout
}

// Hook handles one Claude Code hook payload. It only writes into a board
// directory that already exists. Callers ignore the error: the hook must
// never fail.
func Hook(r io.Reader) error {
	b, err := io.ReadAll(io.LimitReader(r, 16<<20))
	if err != nil {
		return err
	}
	var p hookPayload
	if err := json.Unmarshal(b, &p); err != nil {
		return err
	}
	dir, err := SessionDir(p.SessionID)
	if err != nil {
		return err
	}
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		return errors.New("no board for session")
	}
	switch p.Event {
	case "SubagentStart":
		if p.AgentID == "" {
			return errors.New("no agent_id")
		}
		name := p.AgentType
		if name == "" {
			name = p.AgentID
		}
		return AgentStart(dir, p.AgentID, name, 0)
	case "SubagentStop":
		if p.AgentID == "" {
			return errors.New("no agent_id")
		}
		return AgentStop(dir, p.AgentID, true)
	case "PostToolUse":
		if p.ToolName != "Bash" {
			return nil
		}
		var in struct {
			Command string `json:"command"`
		}
		if json.Unmarshal(p.ToolInput, &in) != nil || !gitCommitRe.MatchString(in.Command) {
			return nil
		}
		_, sha, subject, ok := ParseCommit(toolOutput(p.ToolResponse))
		if !ok {
			return nil
		}
		return AppendLog(dir, sha+" "+subject, "commit")
	}
	return nil
}
