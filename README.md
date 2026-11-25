# AI Board

A live board for each Claude Code session, in the terminal pane next to it: what needs you
on top, then the plan, the PRs, the services it started, its notes and its log. When Claude
needs a decision, you answer it right there.

![A terminal running ai-board on a board titled CSV export for orders: the header shows it waiting on you, with a decision deck to answer, a CI wait and running subagents; below it, the plan with four finished steps folded into one line and the active step highlighted, two pull requests with their CI and review state, two local services probed up and down, and the start of the notes and charts](docs/images/console.svg)

## What you are looking at

![The same board annotated part by part: the title line with total elapsed time and last write, the status line with progress and counts of what is on you, failed, running, external and broken, three slots for what needs you first, then the Plan, PRs, Running, Details and Log blocks, the legend of plan markers and the footer of keys](docs/images/board.svg)

## Get it

```sh
go install github.com/MarioPayan/ai-board/cmd/ai-board@latest
```

Needs Go 1.26 or newer; the binary lands in `$(go env GOPATH)/bin`, which must be on your
`PATH` for the hooks below. Then three steps:

1. **Watch.** Run `ai-board` in a pane next to Claude Code. It follows the newest board in
   `~/.claude/boards/`; `ai-board <session-id>` picks one.
2. **Wire the hooks**, so subagents and commits show up without Claude doing anything. In
   `~/.claude/settings.json`:

   ```json
   {
     "hooks": {
       "SubagentStart": [{ "hooks": [{ "type": "command", "command": "ai-board hook" }] }],
       "SubagentStop": [{ "hooks": [{ "type": "command", "command": "ai-board hook" }] }],
       "PostToolUse": [{ "matcher": "Bash", "hooks": [{ "type": "command", "command": "ai-board hook" }] }]
     }
   }
   ```

3. **Tell Claude to keep the board.** A paragraph in your `CLAUDE.md` does it:

   > Keep a board for this session with `ai-board`. Write the title and status to
   > `~/.claude/boards/$CLAUDE_CODE_SESSION_ID/board.json`, add each step of the plan with
   > `ai-board step add "<step>"` and move it with `ai-board step <n> <state>` (`active`,
   > `waiting` on me, `external`, `done`, `failed`). Track PRs with `ai-board pr set` and
   > local servers with `ai-board run set`. Formats:
   > https://github.com/MarioPayan/ai-board/blob/main/SPEC.md

## Why

| | |
|---|---|
| **What needs you, first** | Six fixed header lines: decks to answer, failures, CI waits and running agents, with counts. They never scroll away |
| **Live** | Re-renders 150 ms after the files go quiet; elapsed times tick every second |
| **Decisions in place** | `D` opens Claude's decision deck: pick, annotate, send. The answers land in a file and on your clipboard, and wake the session waiting on them |
| **Plain files** | One directory per session of JSON, JSONL and Markdown. A file that fails to parse shows its error in place; the rest still renders |
| **Cheap hooks** | `ai-board hook` never fails a hook and only writes to a board that exists. ~13 ms a call: median of 50 runs appending a commit, on an 8-core Linux laptop |
| **Reads, mostly** | Outside the deck view it writes nothing. Its only model call is Suggest in a deck, and only when you press `g` |

## Answering a decision

![The deck view of a deck called Export format: item 1, the date format in the CSV, has option A, ISO 8601, picked and marked recommended with its pros, cons and the reason; option B carries a note; then Other and Suggest another option. Item 2, an approve, reject or defer question about streaming large exports, is still open. The Send button and the deck keys run along the bottom](docs/images/deck.svg)

Claude writes a deck to `decisions/<id>.json` and runs `ai-board answers wait <id>` in the
background. You press `D`, pick with `a`-`d`, write notes, and send with `s`. The answers
reach the session as text; every change autosaves, so quitting loses nothing.

## More

- [Usage](docs/usage.md): every command, key and hook.
- [SPEC.md](SPEC.md): the file formats and the exact layout, the contract between writers
  and the viewer.

## Development

```sh
go test ./...                     # golden renders of testdata/* plus unit tests
go test ./cmd/ai-board -update    # regenerate testdata/*.golden
python3 tools/board-svg.py        # regenerate docs/images/*.svg from the live viewer
```

The README images are captured from the real viewer running on
[tools/demo-board](tools/demo-board) (fictional data), and the script refuses a frame
that differs from `ai-board --once`. Change the layout and rerun it.

## License

MIT, see [LICENSE](LICENSE).
