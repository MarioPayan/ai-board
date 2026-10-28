// Command board renders Claude Code session boards. See SPEC.md.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/kazemu/board/internal/board"
)

const usage = `usage:
  board [<session-id|dir>]                       watch and render
  board --once [--width N] [--now RFC3339] [--expand] [--checks] [--agents] [--legend] [<id>]
                                                 render one frame without color
  board log [--kind K] <text>
  board agent start <id> <name> [--expect MIN]
  board agent stop <id> [--failed]
  board step <index|text-prefix> <state> [--note T] [--link URL] [--dir D]
  board step add <text> [--state S] [--dir D]
  board check [--hours N] [--dir D]              list stuck steps
  board hook                                    read a hook payload on stdin
`

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "hook":
			_ = board.Hook(os.Stdin) // never fail the hook
			return
		case "log":
			exit(cmdLog(os.Args[2:]))
			return
		case "agent":
			exit(cmdAgent(os.Args[2:]))
			return
		case "step":
			exit(cmdStep(os.Args[2:]))
			return
		case "check":
			// a Stop hook calls it: report on stderr, always exit 0
			if err := cmdCheck(os.Args[2:], os.Stdout); err != nil {
				fmt.Fprintln(os.Stderr, "board check:", err)
			}
			return
		}
	}
	exit(cmdView(os.Args[1:], os.Stdout))
}

func exit(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "board:", err)
		os.Exit(1)
	}
}

func cmdView(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("board", flag.ContinueOnError)
	fs.Usage = func() { fmt.Fprint(os.Stderr, usage) }
	once := fs.Bool("once", false, "render one frame and exit")
	width := fs.Int("width", 80, "width for --once")
	nowS := fs.String("now", "", "fixed clock (RFC 3339)")
	expand := fs.Bool("expand", false, "with --once, list every finished step")
	checks := fs.Bool("checks", false, "with --once, list passing checks too")
	agents := fs.Bool("agents", false, "with --once, list every agent")
	legend := fs.Bool("legend", false, "with --once, end with the legend line")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if fs.NArg() > 1 {
		return errors.New("too many arguments")
	}
	dir, name, err := resolve(fs.Arg(0))
	if err != nil {
		return err
	}
	now := time.Now()
	if *nowS != "" {
		if now, err = time.Parse(time.RFC3339, *nowS); err != nil {
			return fmt.Errorf("--now: %w", err)
		}
	}
	if *once {
		fmt.Fprint(stdout, board.Once(board.Load(dir), *width, now, board.Options{Expand: *expand, AllChecks: *checks, AllAgents: *agents, Legend: *legend}))
		return nil
	}
	return board.Run(dir, name)
}

// resolve turns an argument into a board directory: an existing path, a
// session id under ~/.claude/boards, or (empty) the newest board there.
func resolve(arg string) (dir, name string, err error) {
	if arg != "" && (strings.ContainsAny(arg, `/\`) || isDir(arg)) {
		abs, err := filepath.Abs(arg)
		return abs, filepath.Base(abs), err
	}
	if arg != "" {
		dir, err := board.SessionDir(arg)
		return dir, arg, err
	}
	root, err := board.BoardsRoot()
	if err != nil {
		return "", "", err
	}
	ents, err := os.ReadDir(root)
	if err != nil {
		return "", "", fmt.Errorf("no boards: %w", err)
	}
	var best string
	var bestT time.Time
	for _, e := range ents {
		if !e.IsDir() {
			continue
		}
		if fi, err := e.Info(); err == nil && fi.ModTime().After(bestT) {
			best, bestT = e.Name(), fi.ModTime()
		}
	}
	if best == "" {
		return "", "", errors.New("no boards in " + root)
	}
	return filepath.Join(root, best), best, nil
}

func isDir(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.IsDir()
}

func sessionDir() (string, error) {
	id := os.Getenv("CLAUDE_CODE_SESSION_ID")
	if id == "" {
		return "", errors.New("CLAUDE_CODE_SESSION_ID is not set")
	}
	dir, err := board.SessionDir(id)
	if err != nil {
		return "", err
	}
	return dir, os.MkdirAll(dir, 0o755)
}

// takeFlag removes `--name V` / `--name=V` (or a bare boolean `--name`) from args.
func takeFlag(args []string, name string, hasValue bool) (rest []string, val string, found bool, err error) {
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--"+name || a == "-"+name:
			found = true
			if hasValue {
				if i+1 >= len(args) {
					return nil, "", false, fmt.Errorf("--%s needs a value", name)
				}
				i++
				val = args[i]
			}
		case hasValue && (strings.HasPrefix(a, "--"+name+"=") || strings.HasPrefix(a, "-"+name+"=")):
			found, val = true, a[strings.IndexByte(a, '=')+1:]
		default:
			rest = append(rest, a)
		}
	}
	return rest, val, found, nil
}

func cmdLog(args []string) error {
	args, kind, _, err := takeFlag(args, "kind", true)
	if err != nil {
		return err
	}
	text := strings.TrimSpace(strings.Join(args, " "))
	if text == "" {
		return errors.New("usage: board log [--kind K] <text>")
	}
	dir, err := sessionDir()
	if err != nil {
		return err
	}
	return board.AppendLog(dir, text, kind)
}

func cmdAgent(args []string) error {
	if len(args) == 0 {
		return errors.New(usage)
	}
	sub, args := args[0], args[1:]
	switch sub {
	case "start":
		args, exp, found, err := takeFlag(args, "expect", true)
		if err != nil {
			return err
		}
		var min float64
		if found {
			if min, err = strconv.ParseFloat(exp, 64); err != nil || min <= 0 {
				return fmt.Errorf("--expect: want minutes, got %q", exp)
			}
		}
		if len(args) < 2 {
			return errors.New("usage: board agent start <id> <name> [--expect MIN]")
		}
		dir, err := sessionDir()
		if err != nil {
			return err
		}
		return board.AgentStart(dir, args[0], strings.Join(args[1:], " "), min)
	case "stop":
		args, _, failed, err := takeFlag(args, "failed", false)
		if err != nil {
			return err
		}
		if len(args) != 1 {
			return errors.New("usage: board agent stop <id> [--failed]")
		}
		dir, err := sessionDir()
		if err != nil {
			return err
		}
		return board.AgentStop(dir, args[0], !failed)
	}
	return errors.New(usage)
}

// boardDir is dirFlag (created when missing) or, when empty,
// $CLAUDE_CODE_SESSION_ID's board.
func boardDir(dirFlag string) (string, error) {
	if dirFlag == "" {
		return sessionDir()
	}
	return dirFlag, os.MkdirAll(dirFlag, 0o755)
}

func cmdStep(args []string) error {
	args, dirFlag, _, err := takeFlag(args, "dir", true)
	if err != nil {
		return err
	}
	if len(args) > 0 && args[0] == "add" {
		args, state, found, err := takeFlag(args[1:], "state", true)
		if err != nil {
			return err
		}
		if !found {
			state = "todo"
		}
		text := strings.TrimSpace(strings.Join(args, " "))
		if text == "" {
			return errors.New("usage: board step add <text> [--state S]")
		}
		dir, err := boardDir(dirFlag)
		if err != nil {
			return err
		}
		return board.AddStep(dir, text, state, time.Now())
	}
	args, note, hasNote, err := takeFlag(args, "note", true)
	if err != nil {
		return err
	}
	args, link, hasLink, err := takeFlag(args, "link", true)
	if err != nil {
		return err
	}
	if len(args) != 2 {
		return errors.New("usage: board step <index|text-prefix> <state> [--note T] [--link URL]")
	}
	dir, err := boardDir(dirFlag)
	if err != nil {
		return err
	}
	return board.SetStep(dir, args[0], args[1], ptrIf(hasNote, note), ptrIf(hasLink, link), time.Now())
}

func ptrIf(ok bool, s string) *string {
	if !ok {
		return nil
	}
	return &s
}

// cmdCheck prints one line per stuck step (see board.StuckSteps). Without
// --dir and $CLAUDE_CODE_SESSION_ID it prints nothing.
func cmdCheck(args []string, stdout io.Writer) error {
	args, dir, _, err := takeFlag(args, "dir", true)
	if err != nil {
		return err
	}
	_, hours, found, err := takeFlag(args, "hours", true)
	if err != nil {
		return err
	}
	h := 4.0
	if found {
		if h, err = strconv.ParseFloat(hours, 64); err != nil || h <= 0 {
			return fmt.Errorf("--hours: want a positive number, got %q", hours)
		}
	}
	if dir == "" {
		id := os.Getenv("CLAUDE_CODE_SESSION_ID")
		if id == "" {
			return nil
		}
		if dir, err = board.SessionDir(id); err != nil {
			return err
		}
	}
	for _, l := range board.StuckSteps(board.Load(dir), time.Now(), time.Duration(h*float64(time.Hour))) {
		fmt.Fprintln(stdout, l)
	}
	return nil
}
