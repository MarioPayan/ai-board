# AI Board — spec

A terminal viewer for Claude Code session boards, with a decision panel. Claude and hooks write small files into one directory per session; `ai-board` watches that directory and renders it. The viewer only reads, except in the decision panel, where you answer a deck and the viewer writes the draft and the answers under `answers/` (see Decision decks). This file is the contract between the writers and the viewer.

## Directory

```
~/.claude/boards/<session-id>/
  board.json            Claude (whole file, or `ai-board step|status|pending|pr|run`): title, status, steps, pending
  status.json           a Stop hook of your own: latest status, overrides board.json status when newer
  agents.jsonl          hooks + `ai-board agent`: running subagents (append-only)
  log.jsonl             hooks + `ai-board log`: events (append-only)
  sections/
    10-findings.md          free Markdown
    20-space.chart.json     chart
    30-candidates.table.json table
    40-verification.checks.json verification results
  decisions/
    <id>.json           Claude: a decision deck (the web deck's JSON)
  answers/
    <id>-r<round>.txt         the viewer: the answers you sent for that round
    <id>-r<round>.draft.json  the viewer: the answers being drafted, saved on every change
    <id>-r<round>.ack         Claude: a one-line reply to the answers
```

Each file has one kind of writer (under `answers/`, the `.ack` is Claude's and the rest the viewer's). JSONL files are only appended to, never rewritten, so hooks and Claude never race on the same bytes. `board.json` and section files are replaced whole (write to a temp file, rename).

Every file is optional. A missing directory renders a "⏳ waiting for <path>" screen; a missing file renders as empty; a file that fails to parse renders its error in place of its content, and the rest of the board still renders.

## board.json

```json
{
  "title": "CSV export for orders",
  "status": { "state": "working", "text": "Writing the export" },
  "steps": [
    { "text": "Read the orders API", "state": "done", "started": "2026-10-26T11:05:00-05:00", "ended": "2026-10-26T11:10:00-05:00" },
    { "text": "Add the CSV writer", "state": "active", "started": "2026-10-26T11:35:00-05:00" },
    { "text": "Pick the date format", "state": "waiting", "note": "your pick in the deck" },
    { "text": "Update the API docs", "state": "todo" }
  ],
  "pending": [ { "text": "Review the export spec", "link": "https://example.com/export-spec" } ],
  "prs": [ { "repo": "acme/shop", "n": 42, "title": "feat: CSV export for orders", "url": "https://github.com/acme/shop/pull/42", "state": "open", "ci": "pending", "review": "pending" } ],
  "running": [ { "name": "web", "url": "http://localhost:3000", "cmd": "pnpm dev", "login": "demo user in .env.example" } ]
}
```

- `status.json`, written only by the Stop hook, has the same shape as `status` plus a timestamp: `{"state":"working","text":"Awaiting CI","ts":"<RFC3339>"}`. The viewer shows whichever is newer: status.json when its `ts` is after board.json's file mtime, otherwise board.json `status`. A missing or broken status.json falls back to board.json `status`.
- `status.state`: `done` 🟢, `working` 🔵, `waiting` 🟡 (on you), `blocked` 🔴. `status.text` is 1–4 words.
- `steps[].state`: `done` ✅, `active` ▶️, `waiting` (on you) 📥, `external` (CI, an approval, a deploy) ⏳, `failed` ❌, `todo` ⚪, `skipped` (shown like done, struck through). `note` is one clause shown inline after the step text (what it waits on, why it failed, why skipped). `link` is optional, for `external` steps: the CI run, PR or page being awaited, opened from the header slot.
- `started` / `ended` are RFC 3339. The viewer derives times from them; writers never compute durations.
- All steps stay in the list for the whole task. The viewer collapses finished steps into one summary line when there are more than three (see Layout); writers never remove them.
- `prs` (optional): the pull requests the task tracks, one per `repo` + `n`. `state` is `open`, `draft`, `merged` or `closed`; `ci` is `pass`, `fail`, `pending` or empty; `review` is `approved`, `changes` (changes requested), `pending` or empty; `title` and `url` are optional. `ai-board pr set|rm` edits them (see CLI). They render in their own block (see Layout) and do not change the header counts: a failing CI is the PR's own state, already visible in its row, and the step that waits on it is the `external` step the header counts.
- `running` (optional): local services the task started, one per `name`. `url` is optional (the viewer probes it when it is http or https), `cmd` is how it was started (for writers restarting it; not rendered) and `login` is a free-text hint at where the credentials live (`demo user in .env.example`). `login` must never contain a secret (no password, token or key): the board is plain files and is shown on screen. `ai-board run set|rm` edits them.

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
{ "type": "bar", "unit": "s", "series": [ { "label": "10k rows", "value": 3.1 } ] }
{ "type": "sparkline", "values": [3, 5, 2, 8] }
{ "type": "gauge", "label": "Disk", "value": 1.6, "max": 1.9, "unit": "T" }
```

Horizontal bars for `bar` (labels left, value right, bars scaled to the series max). `sparkline` is one line. `gauge` is one labeled bar with `value/max`.

### `*.table.json`

```json
{
  "columns": [ { "name": "Export" }, { "name": "Time", "align": "right", "bar": true } ],
  "rows": [ [ "10k rows", { "v": 3.1, "text": "3.1 s" } ], [ "1M rows", "not measured" ] ]
}
```

A cell is a string, a number, or `{ "v": number, "text": string }`. A `bar: true` column draws an inline bar after the text, scaled to the column's numeric max; non-numeric cells get no bar.

### `*.checks.json`

```json
{ "checks": [ { "name": "go test ./...", "ok": true, "detail": "8 tests" }, { "name": "go vet ./...", "ok": false, "detail": "2 issues" } ] }
```

Verification results. `detail` is optional. When every check passes the section is one green line, `✅ <N>/<N> checks`. When any fails it is a red `❌ <F>/<N> checks failed` followed by only the failing checks, one per line, indented: `❌ <name> — <detail>` (the detail dim, truncated to the width). `c` toggles listing every check, passing ones (`✅`) included, in the live viewer; `--once` lists only the failing ones unless `--checks` is given. An empty list renders a dim `no checks`.

## Decision decks

A deck is `decisions/<id>.json` in the schema of the web decision deck (a separate HTML page, not part of this repository, that reads the same file and is what "the web deck" means below), read unchanged, so one JSON file serves both: `id`, `title`, `project`, `round` (1 when missing), `intro`, and `items[]` with `n`, `title`, `summary`, `details`, `kind` (`choice`, the default, or `confirm`), `options[]` (`key`, `title`, `summary`, `pros`, `cons`, `effort`, `risk`), `recommended`, `why` and `decided`. The panel also reads an optional `url` on the deck and on an item, opened with `o`. `details` is HTML shown as plain text (block ends become line breaks, list items `• `, entities decoded); the mockups (`before`, `mock`) are never rendered. A confirm item has the options `A` Approve, `B` Reject and `C` Defer, as in the web deck. Items with `decided` are listed under "Decided in earlier rounds" and are not asked again. A deck whose `id` is not letters, digits, `.`, `_` and `-` is a parse error. Claude removes the deck file once the deck is closed; a new round is the same file with `round` + 1.

- **Draft**: `answers/<id>-r<round>.draft.json`, `{"choices": {"<n>": "<KEY>" | "other"}, "other": {"<n>": "…"}, "notes": {"<n>": "…"}, "optNotes": {"<n>": {"<KEY>": "…"}}, "general": "…", "extra": {"<n>": [<option>, …]}}`: the web deck's state without its `asks`. `other` holds the Other texts, `optNotes` the notes on options, `extra` the options generated with Suggest (the option schema plus `"generated": true`); the three are omitted when empty, so a draft without them is read as before. The viewer replaces it atomically on every pick, clear and note, and loads it when the deck opens, so a draft survives quitting. A missing or broken draft is an empty one. Drafts are per round, so a new round starts clean.
- **Answers**: Send writes `answers/<id>-r<round>.txt` atomically (temp file, rename): the text the web deck's `serialize()` produces for the same picks and notes, byte for byte, with no trailing newline (the grammar is the one `Serialize` in `internal/board/deck.go` writes, starting `DECISIONS · <id> · round <n>`: Other, item notes, notes on options and generated options included). The web deck's "Ask Claude" questions are not offered here: `serialize()` leaves them out of the text, so they carry nothing to the session. When the round was already sent with other answers, line 2 is `Re-sent: replaces the answers sent <ISO>`, where `<ISO>` is the earlier file's mtime in UTC as JavaScript's `toISOString()` writes it (`2026-11-24T15:00:00.000Z`); sending the same answers again leaves the file untouched. Only the latest answers are kept. The same text goes to the clipboard: OSC 52 to the terminal, and `wl-copy` when it is on PATH.
- **Ack**: Claude may write `answers/<id>-r<round>.ack`, one line (later lines are ignored), after reading the answers: what happens next (`action plan coming`). The deck view and the header slot show it as `Claude: <ack>`.
- Sessions learn of a Send by running `ai-board answers wait <id>` in the background (see CLI).

## Layout

Labels are English; icons replace words where the meaning stays obvious. Colours are ANSI 16-colour indices, so the terminal theme picks the shades. "Darker" below means dimmer than the dim grey (grey and faint): a zero count, an empty slot, a key that would do nothing.

Fixed area: always exactly six lines while a board is loaded, so its shape never changes, top to bottom:

1. **Title line**: `<status emoji> <title>` on the left (the title bold; `⚪` when there is no status); on the right, dimmed, `⏱ <total elapsed> · ↻ <updated ago>`. Total elapsed runs from the first step's `started` (to the last `ended` once the status is `done`); `updated` is the newest mtime in the directory. When the status is `working` and that mtime is more than 10 minutes old, the `↻` part becomes a red `⚠️ stale <ago>`. A part with no data is omitted.
2. **Status line**, indented three spaces: the status text in bold, a 10-cell progress bar and `done/total` (done and skipped count as done). Pinned to the right edge, the counts `📥<N> ❌<N> 🤖<N> ⏳<N> ⚠️<N>`: items on you (pending, plus every deck whose round has no answers file), failures (failed or blocked agents plus failed steps), running agents, external waits (`external` steps), and errors (a board.json error plus every section, deck, agents.jsonl and log.jsonl parse error). A zero count is drawn darker, never left out. When the status is `done`, the line is a green band with the bar full, the counts inside it.
3. **Slots, three lines**: a table of aligned columns with no borders and no header row, `icon | item | detail | right`, filled in priority order:
   - pending items: `📥` and the item in bold across the row; with a link, a dim `↗` on the right and, in color, the item text is a terminal hyperlink (OSC 8) to it (an absolute path as `file://`). The link itself is not printed: cut to the row it reads as nothing;
   - decks: every deck whose round has no answers file (`🙋`, `Deck: <title> · <answered>/<total>` in bold, counting the open items with a pick in the draft), then every sent deck with an ack (`💬`, `Deck: <title>`, and `Claude: <ack>` in magenta as the detail);
   - failures: failed agents (`❌`, the name, a red `failed` and the time it stopped on the right), then failed steps (`❌`, the text, its note as the detail);
   - errors: a board.json, agents.jsonl or deck parse error (`⚠️`, in red);
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
- **Footer**: always exactly one line, every key in a fixed place in three groups: `⇥ sect │ view d done a agt c chk l log D deck g leg │ h help w narrow q` (71 cells, so it fits 80 columns with the indicator). The group name and `│` are dim. A key that would do nothing right now is drawn darker, not left out: `⇥` with fewer than two jump targets or when the whole body fits, `d` with three finished steps or fewer, `a` when every agent is in the header slots, `c` when no checks section has a passing check to reveal, `D` when there is no deck. Pinned to the right edge, dim: `↓ <N>` (lines below the viewport), `end` at the bottom, nothing when the whole body fits. `w` toggles a narrow form with keys only, in the same places: `⇥ │ d a c l D g │ h w q` and the indicator. There is no automatic switch by width; a pane narrower than the line truncates it.

`h` replaces the body with a full-screen help (the fixed area, legend and footer stay): every step marker, status emoji, header, slot and count icon, checks, PR, running, deck and log markers, what each colour means, and every key, deck keys included. `h` again, or `l`, leaves it.

**Deck view** (`D`): one decision at a time, in a region of the body; the fixed area stays. It also follows the decisions by itself: on start and on every reload, when no deck view or list is open and a deck's round has no answers file, the first such deck opens; when the open deck's round gets its answers file (Send) or its file goes away, the view moves to the next waiting deck or, with none left, closes back to the board. `esc` or `D` out of the deck view or the list counts as closing by hand every round waiting at that moment: those rounds do not open by themselves again, a new deck or a new round does, and `D` still opens them. With one deck `D` opens it; with several it first lists them in place of the body (`▸` on the selected one, then on the right its `answered/total`, `sent`, or `💬 <ack>`; `j`/`k` move, `enter` or a click opens, `esc` or `D` goes back), starting on the first deck not yet sent. The deck opens on its first open item.

Where it sits: the body (the rows between the fixed area and the deck footer) is split when it has at least 20 rows. The board's own body, as on the board view, takes the upper third (rounded down) and scrolls on its own with the wheel; a dim `─` rule across the width follows; the deck region takes the rest. Below 20 rows the deck region takes the whole body. While a text box is open its extra footer rows come out of the page, so the split never changes while typing. The deck region, top to bottom:

- **Counter**: `Deck: <title> · <current>/<total>` in bold, prefixed `🙋 ` while the round has no answers file, the position as a plain number (no step marks, no bar), `total` being the open items and the review showing `<total>/<total>`; on the right, dim, `<answered>/<total> answered[ · sent]`.
- **Page**: the decision on screen, or the review. It fills the rows between the counter and the nav line and scrolls (`j`/`k`/`↑`/`↓`/`pgup`/`pgdn`, the wheel); every page change starts it at its top.
- **Nav line**: the Back button on the left (`‹ Back`; ` ‹ Back ` reversed in colour, `[‹ Back]` without, drawn darker on the first decision) and on the right `Next ›`, `Review ›` on the last decision, `Send` on the review, drawn the same way.

A decision page, top to bottom (on the first decision only, first a dim `<project> · round <n>[ · ↗ link (o)]` line, the intro dim and wrapped, the ack as `💬 Claude: <ack>` in magenta, then a blank line):

- `<n>. <title>` in the accent colour, on the right `✓ <n><KEY>` (`✓ <n> Other` for Other) in green or a dim `open`; the summary; `details` as dim plain text; `↗ <url>` dim when the item has one.
- One line per option, the deck's options followed by the ones generated in the draft: `○ <KEY>  <title>` (`●` and the key and title in bold green when picked; ` › ` in the accent colour in place of the indent on the focused option), on a confirm item the dim alias key (`y`, `r`, `f`), `★ recommended` in yellow on the recommended one, `Generated` in magenta on a generated one, a dim `+ note` when the option has no note, and on the right, dim, `effort <S|M|L> · risk <low|med|high>` (the risk word green, yellow or red in colour). Under an option, indented: its summary (dim), `+ <pros joined by ·>` in green, `− <cons>` in red, under the recommended option `why: <why>` dim, and its note as `✎ <note>`.
- After the options, on every item (confirm ones too, as in the web deck): `○ O  Other` (`●` when picked) with its text under it, or a dim `describe what you want instead (click, or O)` while it is picked and empty; then a dim `✦ Suggest another option (g)`, `✦ Thinking…` while Claude is asked. Last, the item note: `✎ <note>` (every line of it) or a dim `✎ add a note (click, or n)`.

The review page, after the last decision: `Review before sending` in the accent colour; `<N> still open.` in yellow followed by a dim `Click one to answer it, or send anyway.`, or a dim `Every decision has an answer.`; after a blank line one row per open item, `✓ <n><KEY> — <title>` with a dim `: <option title>` (`✓ <n> Other — <title>` for Other), or a flagged `○ <n> — <title>  open` in yellow; when some items are decided, `Decided in earlier rounds` with one `✓ <decided>` line each; then `General notes` in bold and its note line. Items with `decided` appear only here.

Text wraps at the width. The deck footer, under the deck region, is two lines: a status line (what the last key did, the send prompt, or a dim hint: `click an option to pick it, ✎ to write a note`, on the review `click a row to answer it again, ⏎ sends`) and the Send button (` Send ` reversed in colour, `[Send]` without) followed by the dim keys `⏎ next ⌫ back  j/k scroll  a-d pick  O other  n N note  R rec  esc` (with the button it fits 80 columns; the other keys are in the help). While a text box is open the footer is six lines: a rule, its name in bold (`Note on <n>. <title>`, `Note on <n><KEY>. <option title>`, `Other for <n>. <title>` or `General notes`), a three-line text box, and `enter save · alt+enter newline · esc cancel`.

Deck keys, when no text box is open: `enter` or `tab` goes to the next decision, from the last one to the review, and on the review sends (asking first like `s`); `backspace` or `shift+tab` goes to the previous one. Picking never moves on by itself. `j`/`k`/`↑`/`↓`, `pgup`/`pgdn` and the wheel scroll the page. `a`–`d` or `1`–`9` pick that option of the decision on screen; the picked key again clears it, and `x` clears it too. On a confirm item `y` approves, `r` rejects and `f` defers. `O` answers with Other and opens the text box for its free text (`O` on an item already answered Other reopens the box; `x` clears the pick and keeps the text, as the web deck keeps it when another option is picked). `←`/`→` move a focus between the options (`←` from none goes to the last), cleared when the page changes; `e` edits the note on the focused option, or, with none focused, on the picked option, else the first. A note on an option is kept and sent whether or not that option is picked. `g` asks Claude for one new option (see Suggest below). On the review these keys do nothing, as there is no decision on screen. `R` fills every open item that has no pick with its `recommended` option, never replacing a pick. `u` goes to the next decision without a pick. `n` edits the item note (on the review, the general note), `N` the general note from anywhere: in every text box `enter` saves (trimmed; an empty text removes the note), `alt+enter` is a newline, `backspace` deletes, `esc` cancels. `o` opens the item's `url`, else the deck's, with `xdg-open` (`open` on macOS). `s` sends from any page; with items still open the footer first asks `<N> open: send anyway? y/n`, and only `y` sends. After a send the status reads `✓ sent <answered>/<total> · copied`. `esc` or `D` goes back to the board; `q` quits (the draft is already saved).

Mouse reporting (cell motion) is on only while the picker or the deck view is open, and off again on the way back, so text selection works on the board. In the deck view the wheel scrolls the part of the body it is over, the board or the page. A click on an option picks it (again clears it) and focuses it, on its `+ note` or its `✎` note lines opens the note on that option without picking it, on the Other lines picks Other and opens its text, on `✦ Suggest another option` asks for one, on a `↗` line opens the url, on a note line opens the note, on a review row goes to that decision, on Back or Next (Review, Send) does what the key does, and on the footer's Send button sends (asking first like `s`).

**Suggest** (`g`, or a click on `✦ Suggest another option`): the web deck's "Suggest another option". The viewer runs `claude -p --output-format json --no-session-persistence --tools ""` off the UI goroutine (2 minute timeout) with, on stdin, the prompt the web deck builds: its `context()` (`Deck: <title>[ (project: <project>)]`, `Item <n>: <title>`, the summary, `Options:` and one `<KEY>. <title> — <summary>` line per option, generated ones included) and then its `Propose ONE new option that differs from all of the above. Reply as JSON: {…}` instruction, word for word. From the JSON result the viewer takes the first `{` to the last `}` of `result` (so a fenced block works) and builds the option as the web deck does: `title` must be a string, `summary`, `effort` and `risk` become strings (`""` when missing), `pros` and `cons` lists of strings, and the key is the letter after the highest key in use (`nextKey()`). It is added to the item in the draft with `"generated": true`, shown with the `Generated` badge, and serializes as `<n><KEY> — <title>: <option title>[ (recommended)] (generated in page)` followed by `   option: <summary>`. While the call runs the item shows `✦ Thinking…` and another `g` on it does nothing; a reply that arrives after the deck view closed still goes into that round's draft. On failure the status line reads the web deck's `No option came back (<code>). Try again.`, the code being `error` (the command failed or reported an error), `bad_json` (no JSON object in the reply) or `bad_shape` (no string `title`).

The six header lines stay fixed; everything below scrolls. Times refresh every second; file changes re-render once events have been quiet for ~150 ms (fsnotify on the directory, `sections/`, `decisions/` and `answers/`), because writers may write in place rather than rename; a parse error from a half-written file stays inline until the next change.

Keys (single keys, no Shift except `shift+tab`; up to four keys arriving in one burst, as `tmux send-keys` sends them, act one at a time; a longer run of characters outside a text box is taken for pasted text and ignored; in a text box or a paste they are text): `j`/`k`/arrows and `pgup`/`pgdn` scroll, `tab`/`shift+tab` (active only when the body scrolls) jump between Agents (when in the body), Plan, PRs, Running, each Details subsection and Log, `l` toggles a full-screen log, `h` toggles the full-screen help, `d` expands or collapses finished steps, `a` lists every agent in the body, `c` lists every check or only the failing ones, `g` shows or hides the legend line, `w` toggles the narrow footer, `D` opens the deck view (keys above), `q` quits.

The viewer sets the terminal title to `board: <session-id>` (kept from before the `ai-board` rename, so a pane of either binary carries it), prefixed `🙋 ` while any deck's round has no answers file, so a pane with decisions waiting stands out in a tab list: your own opener, a script that opens or focuses the board pane, can find an existing pane by that title once it strips the prefix.

## CLI

```
ai-board [<session-id>]          watch and render (default: newest board directory by mtime)
ai-board --once [--width N] [--expand] [--checks] [--agents] [--legend] [--deck ID] [<id>]
                                 render a single frame to stdout without color and exit
ai-board log [--kind K] <text>   append to log.jsonl of $CLAUDE_CODE_SESSION_ID
ai-board agent start <id> <name> [--expect MIN]
ai-board agent stop <id> [--failed]
ai-board step <index|text-prefix> <state> [--note TEXT] [--link URL] [--dir D]
ai-board step add <text> [--state S] [--dir D]
ai-board status <done|working|waiting|blocked> <text> [--dir D]
ai-board pending add <text> [--link URL] [--dir D]
ai-board pending rm <index|text> [--dir D]
ai-board check [--hours N] [--dir D]
ai-board archive [--days N] [--dry-run]
ai-board pr set <repo#n> [--title T] [--url U] [--state S] [--ci C] [--review R] [--dir D]
ai-board pr rm <repo#n> [--dir D]
ai-board run set <name> [--url U] [--cmd C] [--login HINT] [--dir D]
ai-board run rm <name> [--dir D]
ai-board answers wait <deck-id> [--round N] [--dir D]
                                 block until the deck's answers exist, then print them
ai-board hook                    read a Claude Code hook payload on stdin and append events
```

`ai-board answers wait` polls (every second) for `answers/<deck-id>-r<round>.txt` and prints it, with one trailing newline, as soon as it exists, at once when it already does; the round is `--round`, or the round of the deck with that id in `decisions/` (read again on every poll), or 1. `--dir` is the board directory, default `$CLAUDE_CODE_SESSION_ID`'s. A session runs it in the background after writing a deck, so a Send wakes it. A deck id with a path separator is refused.

`ai-board step` edits `board.json` in place of a whole-file write: it reads the file, changes one step and replaces it atomically (temp file, rename). Every field it does not change, including ones this spec does not name, is kept; keys are written in sorted order. A board.json that does not parse is left untouched and the command fails. The step is a 1-based index or a case-insensitive prefix of exactly one step's text. `<state>` is one of the `steps[].state` values. Moving into `active`, `waiting` or `external` sets `started` (RFC 3339, local offset) when it is unset; moving into `done`, `failed` or `skipped` sets `ended` (kept when the step was already in that state); moving into any unfinished state removes `ended`. `--note` / `--link` set those fields, and an empty value removes them. `ai-board step add` appends a step (`todo` unless `--state`), stamped the same way. `--dir` names the board directory; the default is `$CLAUDE_CODE_SESSION_ID`'s, created when missing.

`ai-board status` sets `status.state` (one of the `status.state` values) and `status.text` (the rest of the arguments, spaces allowed, not empty); other fields of `status` are kept. `ai-board pending add` appends `{text, link}` to `pending` (`link` only with a non-empty `--link`). `ai-board pending rm` removes the item named by a 1-based index or, when the argument is not a number, by its exact text (every item with that text; an error when there is none). All three edit board.json like `ai-board step`, with the same `--dir` default.

`ai-board pr set` adds the PR `repo#n` to `prs` or updates it: only the fields given change, and an empty value removes one. Values outside the `prs` enums are refused. `ai-board pr rm` removes it (an error when absent). Both edit board.json like `ai-board step`. `ai-board run set` and `ai-board run rm` do the same for the `running` entry `<name>` (the rest of the arguments, spaces allowed).

`ai-board check` prints one line per step that looks stuck: `active`, `waiting` or `external` for more than `--hours` (default 4) since its `started` (`step <i> "<text>": <state> for <duration>`), and `active` with no `started` (`step <i> "<text>": active with no started`). Without `--dir` it reads `$CLAUDE_CODE_SESSION_ID`'s board, and prints nothing when that is unset too. It exits 1 when it printed any line and 0 when the board is clean; its own errors (a bad flag) go to stderr with exit 0, so a Stop hook that calls it reads a nonzero exit only as stuck steps.

`ai-board archive` moves into `~/.claude/boards/.archive/<name>` every board directory with no file modified in `--days` days (default 7), plus every loose file in `~/.claude/boards/` (the old one-file format, `<id>.md` / `<id>.src.md`) whatever its age. A directory whose `board.json` does not parse is a live board with a writer bug, not an old one, so only idleness archives it. It never touches `$CLAUDE_CODE_SESSION_ID`'s board or files named after it, nor a name already in `.archive` (reported as `skipped`). It prints one line per entry, `archived <name>  <reason>` (`idle <N>d`, `old format`); `--dry-run` prints `would archive …` and moves nothing.

`ai-board hook` handles:

- `SubagentStart` → agent `start` (id `agent_id`). The name is `<agent_type>: <description>`, the description read from `<transcript_path without .jsonl>/subagents/agent-<agent_id>.meta.json` (field `description`), which Claude Code writes before the event; when that file is missing, unreadable or has no description, the name is `agent_type` (or `agent_id` when that is empty too).
- `SubagentStop` → agent `stop`.
- `PostToolUse` for `Bash` whose command runs `git commit` and whose output has git's `[<branch> <sha>] <subject>` line → log entry, `kind: commit`.

It uses the payload's `session_id` to pick the directory. It never fails the hook: on any error it exits 0 and writes nothing. Unknown events are ignored.

`--once` exists for tests and for Claude to check a render; `--now <RFC3339>` fixes the clock for golden tests; `--expand` lists every finished step instead of the collapsed summary; `--checks` lists passing checks too; `--agents` lists every agent in the body; `--legend` ends the frame with the fixed legend line; `--deck <id>` renders the deck view of that deck (by `id`, or file name without `.json`) with its draft, at its first decision: under the fixed area the counter, the whole page and the nav line, without the board body, the split and the footer, and fails when there is no such deck.
