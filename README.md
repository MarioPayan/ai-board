# board

A read-only terminal viewer for Claude Code session boards. Claude and hooks write small files into `~/.claude/boards/<session-id>/`; `board` watches that directory and renders the plan, running subagents, pending items, sections (Markdown, charts, tables) and a log.

The file formats, layout, keys and CLI are defined in [SPEC.md](SPEC.md).

## Install

```sh
GOBIN=$HOME/.local/bin go install ./cmd/board
```

## Usage

```sh
board                      # watch the newest board in ~/.claude/boards
board <session-id>         # watch one session's board
board path/to/board-dir    # watch any board directory

board --once --width 80 [--now 2026-10-26T12:00:00-05:00] <id-or-dir>   # one uncolored frame

board log [--kind commit|note|link] <text>     # append to $CLAUDE_CODE_SESSION_ID's log
board agent start <id> <name> [--expect MIN]
board agent stop <id> [--failed]
board hook                                     # Claude Code hook: reads the payload on stdin
```

Keys: `j`/`k`/arrows and `pgup`/`pgdn` scroll, `tab`/`shift+tab` jump between sections, `l` toggles the full log, `q` quits.

`board hook` handles `SubagentStart`, `SubagentStop` and `PostToolUse` (Bash `git commit`). It only writes into a board directory that already exists and always exits 0.

## Development

```sh
go test ./...                     # golden renders of testdata/* plus unit tests
go test ./cmd/board -update       # regenerate testdata/*.golden
```
