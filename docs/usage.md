# Using ai-board

Every command, key and hook. The file formats and the exact layout are in [SPEC.md](../SPEC.md), the contract between whoever writes a board and the viewer.

## Where boards live

One directory per Claude Code session, `~/.claude/boards/<session-id>/`. Claude writes `board.json` (title, status, steps, pending items, PRs, running services) and drops sections into `sections/`; hooks append to `agents.jsonl` and `log.jsonl`; decks go in `decisions/`. Every file is optional, and a file that fails to parse shows its error in place while the rest of the board still renders.

The commands that write without a `--dir` use `$CLAUDE_CODE_SESSION_ID` to find the board, and create its directory when it is missing.

## Watching

```sh
ai-board                   # watch the newest board in ~/.claude/boards
ai-board <session-id>      # watch one session's board
ai-board path/to/board-dir # watch any board directory

ai-board --once --width 80 [--now 2026-10-26T12:00:00-05:00] [--expand] [--checks] [--agents] [--legend] <id-or-dir>   # one uncolored frame
ai-board --once --deck <deck-id> <id-or-dir>   # one uncolored frame of the deck view
```

`--once` is for tests and for Claude to check a render: it prints one frame without colour and exits.

## Writing

```sh
ai-board step <index|text-prefix> <state> [--note T] [--link URL] [--dir D]   # move a step, stamping started/ended
ai-board step add <text> [--state S] [--dir D] # append a step
ai-board log [--kind commit|note|link] <text>  # append to the log
ai-board agent start <id> <name> [--expect MIN]
ai-board agent stop <id> [--failed]
ai-board pr set <repo#n> [--title T] [--url U] [--state open|draft|merged|closed] [--ci pass|fail|pending] [--review approved|changes|pending]
ai-board pr rm <repo#n>                        # PRs block: one row per PR, CI ✅❌⏳, review 👍👎👀
ai-board run set <name> [--url U] [--cmd C] [--login HINT]   # Running block: ✅ up / ❌ down, probed every 30 s
ai-board run rm <name>
ai-board answers wait <deck-id> [--round N] [--dir D]   # block until the deck is answered, print the answers
ai-board check [--hours 4] [--dir D]           # one line per stuck step; exit 1 when any, 0 when clean
ai-board archive [--days 7] [--dry-run]        # move idle and old-format boards to ~/.claude/boards/.archive
ai-board hook                                  # Claude Code hook: reads the payload on stdin
```

Step states are `todo`, `active`, `waiting` (on you), `external` (CI, an approval, a deploy), `done`, `failed` and `skipped`. `ai-board step` edits `board.json` in place and keeps every field it does not change. A `--login` hint says where credentials live (`user in your password manager`); it must never be a secret, because the board is plain files shown on screen.

## Hooks

`ai-board hook` handles three Claude Code events:

- `SubagentStart`: a running agent, named `<agent_type>: <task description>`.
- `SubagentStop`: that agent stops.
- `PostToolUse` for `Bash` running `git commit`: a commit line in the log.

It only writes into a board directory that already exists and always exits 0, so it can never fail a hook. Wire it in `~/.claude/settings.json`:

```json
{
  "hooks": {
    "SubagentStart": [{ "hooks": [{ "type": "command", "command": "ai-board hook" }] }],
    "SubagentStop": [{ "hooks": [{ "type": "command", "command": "ai-board hook" }] }],
    "PostToolUse": [{ "matcher": "Bash", "hooks": [{ "type": "command", "command": "ai-board hook" }] }]
  }
}
```

`ai-board check` is meant for a `Stop` hook: it exits 1 only when it printed stuck steps (active, waiting or external for longer than `--hours`).

## Board keys

| Key | Does |
|---|---|
| `j` `k`, arrows, `pgup` `pgdn` | scroll |
| `tab` `shift+tab` | jump between blocks |
| `d` | expand or collapse finished steps |
| `a` | list every agent in the body |
| `c` | list every check, or only the failing ones |
| `l` | full-screen log |
| `g` | show or hide the legend line |
| `h` | full-screen help: every marker, icon, colour and key |
| `w` | narrow footer, keys only |
| `D` | open the deck view (a list first when there are several decks) |
| `q` | quit |

The six-line header (title, status with counts, three slots for what needs attention, a rule) and the one-line footer keep the same shape. The footer shows every key in a fixed place, darker when it would do nothing, and `↓ N` or `end` on the right while the body scrolls. The mouse is on only in the deck view, so text selection keeps working on the board.

## Decision decks

A deck is `decisions/<id>.json`, a list of items with options, pros, cons and a recommendation. `D` opens it in the lower two thirds of the body, under the board (the whole body when the pane is short), one decision at a time: `Deck: <title> · <current>/<total>` on top, Back and Next at the bottom, and after the last decision a review of every pick with the general note and Send. You pick, write notes and send; the viewer writes the answers to `answers/<id>-r<round>.txt` and copies them to the clipboard (OSC 52, and `wl-copy` when it is installed). A session that ran `ai-board answers wait <id>` in the background wakes up with the answers. Every change autosaves to a draft, so quitting loses nothing, and a one-line `answers/<id>-r<round>.ack` from Claude shows as `Claude: <ack>`.

| Key | Does |
|---|---|
| `enter` `tab` | next decision; after the last one the review, where `enter` sends |
| `backspace` `shift+tab` | previous decision |
| `j` `k` `pgup` `pgdn` | scroll the decision (the wheel too) |
| `a`-`d`, `1`-`9` | pick that option; the same key again, or `x`, clears it |
| `y` `r` `f` | approve, reject or defer a confirm item |
| `O` | answer with Other and write what you want instead |
| `←` `→`, then `e` | focus an option and write a note on it, picked or not |
| `g` | Suggest: ask `claude -p` for one more option |
| `R` | fill every open item with its recommendation, never replacing a pick |
| `u` | jump to the next unanswered decision |
| `n` `N` | the item note, the general note (in the box enter saves, alt+enter newline, esc cancels) |
| `o` | open the item's or the deck's link |
| `s` | send (asks first when items are still open) |
| `esc` `D` | back to the board |

Picking never moves on by itself. Clicks work too: an option picks it, `+ note` writes a note, `✦ Suggest another option` asks for one, Back and Next move, a review row goes back to that decision, `[Send]` sends.
