# AI Board — spec

A read-only terminal viewer for Claude Code session boards. Claude and hooks write small files into one directory per session; `ai-board` watches that directory and renders it. This file is the contract between the writers and the viewer.

## Directory

```
~/.claude/boards/<session-id>/
  board.json            Claude (whole file, or `ai-board step`): title, status, steps, pending
  status.json           Stop hook (lib/meta/stop-guard.py): latest status, overrides board.json status when newer
  agents.jsonl          hooks + `ai-board agent`: running subagents (append-only)
  log.jsonl             hooks + `ai-board log`: events (append-only)
  sections/
    10-findings.md          free Markdown
    20-space.chart.json     chart
    30-candidates.table.json table
    40-verification.checks.json verification results
```

Each file has one kind of writer. JSONL files are only appended to, never rewritten, so hooks and Claude never race on the same bytes. `board.json` and section files are replaced whole (write to a temp file, rename).

Every file is optional. A missing directory renders a "⏳ waiting for <path>" screen; a missing file renders as empty; a file that fails to parse renders its error in place of its content, and the rest of the board still renders.

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
  "pending": [ { "text": "Aprobar el deck", "link": "https://claude.ai/..." } ],
  "prs": [ { "repo": "kazemu/board", "n": 12, "title": "feat(tui): PRs section", "url": "https://github.com/kazemu/board/pull/12", "state": "open", "ci": "pending", "review": "pending" } ],
  "running": [ { "name": "web", "url": "http://localhost:3000", "cmd": "pnpm dev", "login": "user in 1Password: Board dev" } ]
}
```

- `status.json`, written only by the Stop hook, has the same shape as `status` plus a timestamp: `{"state":"working","text":"Awaiting CI","ts":"<RFC3339>"}`. The viewer shows whichever is newer: status.json when its `ts` is after board.json's file mtime, otherwise board.json `status`. A missing or broken status.json falls back to board.json `status`.
- `status.state`: `done` 🟢, `working` 🔵, `waiting` 🟡 (on Mario), `blocked` 🔴. Same meaning as the response status line. `status.text` is 1–4 words.
- `steps[].state`: `done` ✅, `active` ▶️, `waiting` (on Mario) 📥, `external` (CI, an approval, a deploy) ⏳, `failed` ❌, `todo` ⚪, `skipped` (shown like done, struck through). `note` is one clause shown inline after the step text (what it waits on, why it failed, why skipped). `link` is optional, for `external` steps: the CI run, PR or page being awaited, opened from the header slot.
- `started` / `ended` are RFC 3339. The viewer derives times from them; writers never compute durations.
- All steps stay in the list for the whole task. The viewer collapses finished steps into one summary line when there are more than three (see Layout); writers never remove them.
- `prs` (optional): the pull requests the task tracks, one per `repo` + `n`. `state` is `open`, `draft`, `merged` or `closed`; `ci` is `pass`, `fail`, `pending` or empty; `review` is `approved`, `changes` (changes requested), `pending` or empty; `title` and `url` are optional. `ai-board pr set|rm` edits them (see CLI). They render in their own block (see Layout) and do not change the header counts: a failing CI is the PR's own state, already visible in its row, and the step that waits on it is the `external` step the header counts.
- `running` (optional): local services the task started, one per `name`. `url` is optional (the viewer probes it when it is http or https), `cmd` is how it was started (for writers restarting it; not rendered) and `login` is a free-text hint at where the credentials live (`user in 1Password: Foo`). `login` must never contain a secret (no password, token or key): the board is plain files and is shown on screen. `ai-board run set|rm` edits them.

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

Rendered with Glamour. Mermaid is not rendered; it shows as a code block. Inline code has no padding; in the live viewer it is cyan with no background (both dark and light styles).

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

### `*.checks.json`

```json
{ "checks": [ { "name": "go test ./...", "ok": true, "detail": "8 tests" }, { "name": "go vet ./...", "ok": false, "detail": "2 issues" } ] }
```

Verification results. `detail` is optional. When every check passes the section is one green line, `✅ <N>/<N> checks`. When any fails it is a red `❌ <F>/<N> checks failed` followed by only the failing checks, one per line, indented: `❌ <name> — <detail>` (the detail dim, truncated to the width). `c` toggles listing every check, passing ones (`✅`) included, in the live viewer; `--once` lists only the failing ones unless `--checks` is given. An empty list renders a dim `no checks`.

## Layout

Labels are English; icons replace words where the meaning stays obvious. Colours are ANSI 16-colour indices, so the terminal theme picks the shades. "Darker" below means dimmer than the dim grey (grey and faint): a zero count, an empty slot, a key that would do nothing.

Fixed area: always exactly six lines while a board is loaded, so its shape never changes, top to bottom:

1. **Title line**: `<status emoji> <title>` on the left (the title bold; `⚪` when there is no status); on the right, dimmed, `⏱ <total elapsed> · ↻ <updated ago>`. Total elapsed runs from the first step's `started` (to the last `ended` once the status is `done`); `updated` is the newest mtime in the directory. When the status is `working` and that mtime is more than 10 minutes old, the `↻` part becomes a red `⚠️ stale <ago>`. A part with no data is omitted.
2. **Status line**, indented three spaces: the status text in bold, a 10-cell progress bar and `done/total` (done and skipped count as done). Pinned to the right edge, the counts `📥<N> ❌<N> 🤖<N> ⏳<N> ⚠️<N>`: items on you (pending), failures (failed or blocked agents plus failed steps), running agents, external waits (`external` steps), and errors (a board.json error plus every section, agents.jsonl and log.jsonl parse error). A zero count is drawn darker, never left out. When the status is `done`, the line is a green band with the bar full, the counts inside it.
3. **Slots, three lines**: a table of aligned columns with no borders and no header row, `icon | item | detail | right`, filled in priority order:
   - pending items: `📥` and the item in bold across the row; with a link, a dim `↗` on the right and, in color, the item text is a terminal hyperlink (OSC 8) to it (an absolute path as `file://`). The link itself is not printed: cut to the row it reads as nothing;
   - failures: failed agents (`❌`, the name, a red `failed` and the time it stopped on the right), then failed steps (`❌`, the text, its note as the detail);
   - errors: a board.json or agents.jsonl parse error (`⚠️`, in red);
   - external waits: every `external` step (`⏳`, the text, its note in magenta as the detail, i.e. who or what is awaited); on the right, dim, the time since its `started` and, with a `link`, a `↗`, the text then being a hyperlink to it like a pending item;
   - running agents (`🤖`), sorted over their expected time first, then by elapsed time, longest first; on the right the elapsed time or, with `expect_min`, a bar of elapsed against expected (full and marked `over` when exceeded).

   When there are more items than slots, the last row's right column ends with a dim `+<N>`, the number of items not shown. Unused slots are blank lines, so the fixed area keeps its height; the zero counts on the status line say which categories are empty.
4. **Rule**: a dim `─` across the full width.

A missing directory renders only the `⏳ waiting for <path>` line instead.

Scrolling body, top to bottom; each heading is a bold title followed by a dim rule, and a block with nothing to show is omitted. The first block sits right under the fixed area's rule; one blank line separates each top-level block (Agents, Plan, PRs, Running, Details, Log) from the next; a subsection title sits directly on its body, with no blank line:

5. **🤖 Agents**: every agent (failed first, then as in the slots, with any agents.jsonl parse errors), only when `a` (live viewer) or `--agents` (`--once`) is on and some agent is not in the header slots. The slots keep showing the top items.
6. **📋 Plan**: every step as `<marker> <text> — <note>`, the note coloured by state (waiting yellow, failed red, external magenta, others dim). The right column means one thing per state: done and skipped show their duration (`ended - started`, blank when either is missing); active shows `⏱ <elapsed>`; other states show nothing. The active step's marker is `▶` in an accent colour and, in the live viewer, its whole row has a subtle background. When a line is too long the note is truncated first (down to 6 cells), then the text (down to 20 cells); if it still does not fit, the note is dropped.
   When more than three steps are done or skipped, they collapse into one line, `✅ <N> · last: <text of the most recently ended one>` with that step's duration on the right, placed where the first finished step was. `d` toggles this in the live viewer; `--once` renders collapsed unless `--expand` is given.
7. **🔀 PRs**: one row per `prs` entry, in file order: `<repo>#<n> <title>` on the left (the title cut with `…` to fit; in color a terminal hyperlink to `url`, like a pending item; dim when merged or closed), and on the right, dim, in columns aligned across rows: the state word (`draft`, `merged`, `closed`; none for `open`), CI (`✅` pass, `❌` fail, `⏳` pending), review (`👍` approved, `👎` changes requested, `👀` pending) and `↗` when there is a url. A column no row uses takes no space. After the plan because it is the plan's output, before Details because it is live state rather than notes.
8. **🚀 Running**: one row per `running` entry, in file order: `✅` up, `❌` down or a dim `?` (not probed: no http(s) url, or no probe answered yet), the name in bold, the url dim (in color a terminal hyperlink to it), and on the right, dim, the `login` hint. The url is cut first, then the name. The live viewer probes every http(s) url when it starts and 30 s after each round answers, off the render path (an HTTP HEAD, a GET when HEAD gets 405, 2 s timeout; any status below 500 is up, so a login wall counts as up). `--once` probes the same way once before rendering, so it takes up to 2 s more when a url does not answer. Like PRs, it does not change the header counts. After PRs, both being live state the plan produced.
9. **🔎 Details**: every file in `sections/`, in order, each under its own title as a bold subheading.
10. **📜 Log**: the five newest entries.

Live viewer only, below the body:

- **Legend** (hidden by default; `g` shows or hides it): one fixed dim line naming the plan markers, `▶️ active · 📥 on you · ⏳ external · ❌ failed · ⚪ todo · ✅ done`, just above the footer; the body gives up a line for it. Entries that do not fit the width are dropped from the end and replaced by `…`. `--once --legend` appends the same line after a blank line.
- **Footer**: always exactly one line, every key in a fixed place in three groups: `⇥ sect │ view d done a agents c checks l log g legend │ h help w narrow q` (73 cells, so it fits 80 columns with the indicator). The group name and `│` are dim. A key that would do nothing right now is drawn darker, not left out: `⇥` with fewer than two jump targets or when the whole body fits, `d` with three finished steps or fewer, `a` when every agent is in the header slots, `c` when no checks section has a passing check to reveal. Pinned to the right edge, dim: `↓ <N>` (lines below the viewport), `end` at the bottom, nothing when the whole body fits. `w` toggles a narrow form with keys only, in the same places: `⇥ │ d a c l g │ h w q` and the indicator. There is no automatic switch by width; a pane narrower than the line truncates it.

`h` replaces the body with a full-screen help (the fixed area, legend and footer stay): every step marker, status emoji, header, slot and count icon, checks, PR, running and log markers, what each colour means, and every key. `h` again, or `l`, leaves it.

The six header lines stay fixed; everything below scrolls. Times refresh every second; file changes re-render once events have been quiet for ~150 ms (fsnotify on the directory and `sections/`), because writers may write in place rather than rename; a parse error from a half-written file stays inline until the next change.

Keys (single keys, no Shift except `shift+tab`): `j`/`k`/arrows and `pgup`/`pgdn` scroll, `tab`/`shift+tab` (active only when the body scrolls) jump between Agents (when in the body), Plan, PRs, Running, each Details subsection and Log, `l` toggles a full-screen log, `h` toggles the full-screen help, `d` expands or collapses finished steps, `a` lists every agent in the body, `c` lists every check or only the failing ones, `g` shows or hides the legend line, `w` toggles the narrow footer, `q` quits.

The viewer sets the terminal title to `board: <session-id>` (kept from before the `ai-board` rename, so the opener finds a pane of either binary); the opener script finds an existing pane by that title.

## CLI

```
ai-board [<session-id>]          watch and render (default: newest board directory by mtime)
ai-board --once [--width N] [--expand] [--checks] [--agents] [--legend] [<id>]
                                 render a single frame to stdout without color and exit
ai-board log [--kind K] <text>   append to log.jsonl of $CLAUDE_CODE_SESSION_ID
ai-board agent start <id> <name> [--expect MIN]
ai-board agent stop <id> [--failed]
ai-board step <index|text-prefix> <state> [--note TEXT] [--link URL] [--dir D]
ai-board step add <text> [--state S] [--dir D]
ai-board check [--hours N] [--dir D]
ai-board archive [--days N] [--dry-run]
ai-board pr set <repo#n> [--title T] [--url U] [--state S] [--ci C] [--review R] [--dir D]
ai-board pr rm <repo#n> [--dir D]
ai-board run set <name> [--url U] [--cmd C] [--login HINT] [--dir D]
ai-board run rm <name> [--dir D]
ai-board hook                    read a Claude Code hook payload on stdin and append events
```

`ai-board step` edits `board.json` in place of a whole-file write: it reads the file, changes one step and replaces it atomically (temp file, rename). Every field it does not change, including ones this spec does not name, is kept; keys are written in sorted order. A board.json that does not parse is left untouched and the command fails. The step is a 1-based index or a case-insensitive prefix of exactly one step's text. `<state>` is one of the `steps[].state` values. Moving into `active`, `waiting` or `external` sets `started` (RFC 3339, local offset) when it is unset; moving into `done`, `failed` or `skipped` sets `ended` (kept when the step was already in that state); moving into any unfinished state removes `ended`. `--note` / `--link` set those fields, and an empty value removes them. `ai-board step add` appends a step (`todo` unless `--state`), stamped the same way. `--dir` names the board directory; the default is `$CLAUDE_CODE_SESSION_ID`'s, created when missing.

`ai-board pr set` adds the PR `repo#n` to `prs` or updates it: only the fields given change, and an empty value removes one. Values outside the `prs` enums are refused. `ai-board pr rm` removes it (an error when absent). Both edit board.json like `ai-board step`. `ai-board run set` and `ai-board run rm` do the same for the `running` entry `<name>` (the rest of the arguments, spaces allowed).

`ai-board check` prints one line per step that looks stuck: `active`, `waiting` or `external` for more than `--hours` (default 4) since its `started` (`step <i> "<text>": <state> for <duration>`), and `active` with no `started` (`step <i> "<text>": active with no started`). Without `--dir` it reads `$CLAUDE_CODE_SESSION_ID`'s board, and prints nothing when that is unset too. It exits 1 when it printed any line and 0 when the board is clean; its own errors (a bad flag) go to stderr with exit 0, so a Stop hook that calls it reads a nonzero exit only as stuck steps.

`ai-board archive` moves into `~/.claude/boards/.archive/<name>` every board directory with no file modified in `--days` days (default 7), plus every loose file in `~/.claude/boards/` (the old one-file format, `<id>.md` / `<id>.src.md`) whatever its age. A directory whose `board.json` does not parse is a live board with a writer bug, not an old one, so only idleness archives it. It never touches `$CLAUDE_CODE_SESSION_ID`'s board or files named after it, nor a name already in `.archive` (reported as `skipped`). It prints one line per entry, `archived <name>  <reason>` (`idle <N>d`, `old format`); `--dry-run` prints `would archive …` and moves nothing.

`ai-board hook` handles:

- `SubagentStart` → agent `start` (id `agent_id`). The name is `<agent_type>: <description>`, the description read from `<transcript_path without .jsonl>/subagents/agent-<agent_id>.meta.json` (field `description`), which Claude Code writes before the event; when that file is missing, unreadable or has no description, the name is `agent_type` (or `agent_id` when that is empty too).
- `SubagentStop` → agent `stop`.
- `PostToolUse` for `Bash` whose command runs `git commit` and whose output has git's `[<branch> <sha>] <subject>` line → log entry, `kind: commit`.

It uses the payload's `session_id` to pick the directory. It never fails the hook: on any error it exits 0 and writes nothing. Unknown events are ignored.

`--once` exists for tests and for Claude to check a render; `--now <RFC3339>` fixes the clock for golden tests; `--expand` lists every finished step instead of the collapsed summary; `--checks` lists passing checks too; `--agents` lists every agent in the body; `--legend` ends the frame with the fixed legend line.
