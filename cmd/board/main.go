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
  board --once [--width N] [--now RFC3339] [--expand] [<id>]
                                                 render one frame without color
  board log [--kind K] <text>
  board agent start <id> <name> [--expect MIN]
  board agent stop <id> [--failed]
  board hook                                     read a hook payload on stdin
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
		fmt.Fprint(stdout, board.Once(board.Load(dir), *width, now, *expand))
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
