# board — spec

A read-only terminal viewer for Claude Code session boards. Claude and hooks write small files into one directory per session; `board` watches that directory and renders it. This file is the contract between the writers and the viewer.

## Directory

```
~/.claude/boards/<session-id>/
  board.json            Claude: title, status, steps, pending
  status.json           Stop hook (lib/meta/stop-guard.py): latest status, overrides board.json status when newer
  agents.jsonl          hooks + `board agent`: running subagents (append-only)
  log.jsonl             hooks + `board log`: events (append-only)
  sections/
    10-findings.md          free Markdown
    20-space.chart.json     chart
    30-candidates.table.json table
```

Each file has one kind of writer. JSONL files are only appended to, never rewritten, so hooks and Claude never race on the same bytes. `board.json` and section files are replaced whole (write to a temp file, rename).

Every file is optional. A missing directory renders a "waiting for <path>" screen; a missing file renders as empty; a file that fails to parse renders its error in place of its content, and the rest of the board still renders.

## board.json

```json
{
  "title": "Limpieza de disco",
  "status": { "state": "working", "text": "Clasificando hallazgos" },
  "steps": [
    { "text": "Leer guías de deck y board", "state": "done", "started": "2026-10-26T11:05:00-05:00", "ended": "2026-10-26T11:10:00-05:00" },
    { "text": "Clasificar candidatos con riesgo", "state": "active", "started": "2026-10-26T11:35:00-05:00" },
    { "text": "Construir decision deck", "state": "waiting", "note": "tu OK a la lista" },
    { "text": "Publicar artefacto", "state": "todo" }
  ],
  "pending": [ { "text": "Aprobar el deck", "link": "https://claude.ai/..." } ]
}
```

- `status.json`, written only by the Stop hook, has the same shape as `status` plus a timestamp: `{"state":"working","text":"Awaiting CI","ts":"<RFC3339>"}`. The viewer shows whichever is newer: status.json when its `ts` is after board.json's file mtime, otherwise board.json `status`. A missing or broken status.json falls back to board.json `status`.
- `status.state`: `done` 🟢, `working` 🔵, `waiting` 🟡 (on Mario), `blocked` 🔴. Same meaning as the response status line. `status.text` is 1–4 words.
- `steps[].state`: `done` ✔, `active` ●, `waiting` (on Mario) 🟡, `external` (CI, a subagent, a deploy) 🟣, `failed` ✘, `todo` ○, `skipped` (shown like done, struck through). `note` is one clause shown after the step (what it waits on, why it failed, why skipped).
- `started` / `ended` are RFC 3339. The viewer derives times from them; writers never compute durations.
- All steps stay in the list for the whole task. Done steps are dimmed, not removed.

## agents.jsonl

One JSON object per line:

```json
{"ts":"2026-10-26T13:20:00-05:00","event":"start","id":"a1b2","name":"repo-worker: TUI","expect_min":30}
{"ts":"2026-10-26T13:52:00-05:00","event":"stop","id":"a1b2","ok":true}
```

An agent is running when its last `start` has no later `stop` with the same `id`. `expect_min` is optional. With it the viewer draws a bar of elapsed against expected (full and marked `over` when exceeded); without it, only the elapsed time. A stop with `ok:false` keeps the agent visible as failed 🔴 until a later `start` or `dismiss` event with that `id`.

## log.jsonl

```json
{"ts":"2026-10-26T13:31:00-05:00","text":"a3f9c1e feat: render tables","kind":"commit"}
```

`kind` is optional (`commit`, `note`, `link`); it only changes the marker. Shown newest first.

## Sections

Files in `sections/`, ordered by file name. The title is the file name without the numeric prefix and the extension, with `-` turned into spaces and the first letter capitalized, unless a Markdown file starts with a `#` heading, which is then the title.

### `*.md`

Rendered with Glamour. Mermaid is not rendered; it shows as a code block.

### `*.chart.json`

```json
{ "type": "bar", "unit": "G", "series": [ { "label": "Docker volúmenes", "value": 384 } ] }
{ "type": "sparkline", "values": [3, 5, 2, 8] }
{ "type": "gauge", "label": "Disco", "value": 1.6, "max": 1.9, "unit": "T" }
```

Horizontal bars for `bar` (labels left, value right, bars scaled to the series max). `sparkline` is one line. `gauge` is one labeled bar with `value/max`.

### `*.table.json`

```json
{
  "columns": [ { "name": "Zona" }, { "name": "Recuperable", "align": "right", "bar": true } ],
  "rows": [ [ "Docker volúmenes huérfanos", { "v": 384, "text": "384G" } ], [ "556 worktrees", "por medir" ] ]
}
```

A cell is a string, a number, or `{ "v": number, "text": string }`. A `bar: true` column draws an inline bar after the text, scaled to the column's numeric max; non-numeric cells get no bar.

## Layout

Top to bottom:

1. **Header**: title; status emoji, status text, a 10-cell progress bar and `done/total` (done and skipped count as done); total elapsed since the first step's `started`; `updated <ago>` from the newest mtime in the directory.
2. **Agents**: one line per running or failed agent. Omitted when there are none.
3. **Plan**: every step with its marker, its text, and on the right the end time (done), elapsed time (active), or note.
4. **Pending**: one line per item. When empty, a single dimmed `nothing pending` line.
5. **Sections**, in order, each with its title as a rule.
6. **Log**: the five newest entries.

Header and agents stay fixed; everything below scrolls. Times refresh every second; file changes re-render once events have been quiet for ~150 ms (fsnotify on the directory and `sections/`), because writers may write in place rather than rename; a parse error from a half-written file stays inline until the next change.

Keys: `j`/`k`/arrows and `pgup`/`pgdn` scroll, `tab`/`shift+tab` jump between sections, `l` toggles a full-screen log, `q` quits.

The viewer sets the terminal title to `board: <session-id>`; the opener script finds an existing pane by that title.

## CLI

```
board [<session-id>]             watch and render (default: newest board directory by mtime)
board --once [--width N] [<id>]  render a single frame to stdout without color and exit
board log [--kind K] <text>      append to log.jsonl of $CLAUDE_CODE_SESSION_ID
board agent start <id> <name> [--expect MIN]
board agent stop <id> [--failed]
board hook                       read a Claude Code hook payload on stdin and append events
```

`board hook` handles:

- `SubagentStart` → agent `start` (id `agent_id`, name `agent_type`).
- `SubagentStop` → agent `stop`.
- `PostToolUse` for `Bash` whose command runs `git commit` and whose output has git's `[<branch> <sha>] <subject>` line → log entry, `kind: commit`.

It uses the payload's `session_id` to pick the directory. It never fails the hook: on any error it exits 0 and writes nothing. Unknown events are ignored.

`--once` exists for tests and for Claude to check a render; `--now <RFC3339>` fixes the clock for golden tests.
