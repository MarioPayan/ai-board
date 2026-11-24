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

	"github.com/MarioPayan/ai-board/internal/board"
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
  board archive [--days N] [--dry-run]           move idle and old-format boards to .archive
  board pr set <repo#n> [--title T] [--url U] [--state S] [--ci C] [--review R] [--dir D]
  board pr rm <repo#n> [--dir D]
  board run set <name> [--url U] [--cmd C] [--login HINT] [--dir D]
  board run rm <name> [--dir D]
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
		case "pr":
			exit(cmdPR(os.Args[2:]))
			return
		case "run":
			exit(cmdRun(os.Args[2:]))
			return
		case "archive":
			exit(cmdArchive(os.Args[2:], os.Stdout))
			return
		case "check":
			// a Stop hook calls it: exit 1 means stuck steps were printed;
			// its own errors go to stderr with exit 0, never read as problems
			n, err := cmdCheck(os.Args[2:], os.Stdout)
			if err != nil {
				fmt.Fprintln(os.Stderr, "board check:", err)
			}
			if n > 0 {
				os.Exit(1)
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
		snap := board.Load(dir)
		up := board.ProbeAll(snap.Board.Running) // once, 2 s at most
		fmt.Fprint(stdout, board.Once(snap, *width, now, board.Options{Expand: *expand, AllChecks: *checks, AllAgents: *agents, Legend: *legend, Up: up}))
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

// takeFields removes `--<name> V` for each name from args and returns the
// values given, keyed by name.
func takeFields(args []string, names ...string) ([]string, map[string]string, error) {
	fields := map[string]string{}
	for _, n := range names {
		var v string
		var found bool
		var err error
		if args, v, found, err = takeFlag(args, n, true); err != nil {
			return nil, nil, err
		}
		if found {
			fields[n] = v
		}
	}
	return args, fields, nil
}

func cmdPR(args []string) error {
	const use = "usage: board pr set <repo#n> [--title T] [--url U] [--state S] [--ci C] [--review R] [--dir D]\n       board pr rm <repo#n> [--dir D]"
	args, fields, err := takeFields(args, "dir", "title", "url", "state", "ci", "review")
	if err != nil {
		return err
	}
	dirFlag := fields["dir"]
	delete(fields, "dir")
	if len(args) != 2 || (args[0] != "set" && args[0] != "rm") || (args[0] == "rm" && len(fields) > 0) {
		return errors.New(use)
	}
	repo, n, err := board.ParsePRRef(args[1])
	if err != nil {
		return err
	}
	dir, err := boardDir(dirFlag)
	if err != nil {
		return err
	}
	if args[0] == "rm" {
		return board.RmPR(dir, repo, n)
	}
	return board.SetPR(dir, repo, n, fields)
}

func cmdRun(args []string) error {
	const use = "usage: board run set <name> [--url U] [--cmd C] [--login HINT] [--dir D]\n       board run rm <name> [--dir D]"
	args, fields, err := takeFields(args, "dir", "url", "cmd", "login")
	if err != nil {
		return err
	}
	dirFlag := fields["dir"]
	delete(fields, "dir")
	if len(args) < 2 || (args[0] != "set" && args[0] != "rm") || (args[0] == "rm" && len(fields) > 0) {
		return errors.New(use)
	}
	name := strings.Join(args[1:], " ")
	dir, err := boardDir(dirFlag)
	if err != nil {
		return err
	}
	if args[0] == "rm" {
		return board.RmRunning(dir, name)
	}
	return board.SetRunning(dir, name, fields)
}

func cmdArchive(args []string, stdout io.Writer) error {
	args, _, dry, _ := takeFlag(args, "dry-run", false)
	args, days, found, err := takeFlag(args, "days", true)
	if err != nil {
		return err
	}
	if len(args) > 0 {
		return errors.New("usage: board archive [--days N] [--dry-run]")
	}
	d := 7.0
	if found {
		if d, err = strconv.ParseFloat(days, 64); err != nil || d < 0 {
			return fmt.Errorf("--days: want a number of days, got %q", days)
		}
	}
	root, err := board.BoardsRoot()
	if err != nil {
		return err
	}
	lines, err := board.Archive(root, os.Getenv("CLAUDE_CODE_SESSION_ID"), time.Duration(d*24*float64(time.Hour)), time.Now(), dry)
	for _, l := range lines {
		fmt.Fprintln(stdout, l)
	}
	return err
}

// cmdCheck prints one line per stuck step (see board.StuckSteps). Without
// --dir and $CLAUDE_CODE_SESSION_ID it prints nothing. It returns the
// number of lines printed.
func cmdCheck(args []string, stdout io.Writer) (int, error) {
	args, dir, _, err := takeFlag(args, "dir", true)
	if err != nil {
		return 0, err
	}
	_, hours, found, err := takeFlag(args, "hours", true)
	if err != nil {
		return 0, err
	}
	h := 4.0
	if found {
		if h, err = strconv.ParseFloat(hours, 64); err != nil || h <= 0 {
			return 0, fmt.Errorf("--hours: want a positive number, got %q", hours)
		}
	}
	if dir == "" {
		id := os.Getenv("CLAUDE_CODE_SESSION_ID")
		if id == "" {
			return 0, nil
		}
		if dir, err = board.SessionDir(id); err != nil {
			return 0, err
		}
	}
	lines := board.StuckSteps(board.Load(dir), time.Now(), time.Duration(h*float64(time.Hour)))
	for _, l := range lines {
		fmt.Fprintln(stdout, l)
	}
	return len(lines), nil
}
