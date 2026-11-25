#!/usr/bin/env python3
"""Generate the README images in docs/images/ from the real viewer.

    python3 tools/board-svg.py           # rebuild ai-board, capture, write the SVGs
    python3 tools/board-svg.py --dump    # also print each captured screen as text

Nothing here describes the board from memory. The script builds ./cmd/ai-board,
runs the live viewer on tools/demo-board (fictional data) inside a pty of a
fixed size, presses the keys a shot needs and draws the cells the terminal
ended up holding: every character, colour and bold is what the binary printed.

Two things keep the pictures honest:

- Before drawing, each captured screen is compared line by line with
  `ai-board --once` on the same board at the same instant. A frame that does
  not match the renderer's own text output is refused.
- The fixture's timestamps are shifted so that, at capture time, the board is
  exactly as old as it is at its anchor (12:40 on its own clock), with the
  wall-clock times kept. The live viewer reads the real clock, so this is what
  makes the images the same on every run.

Only the colours are a choice: the viewer prints ANSI 16-colour indices and
the terminal theme picks the shades, so PALETTE below is one dark theme.
Direct fill= attributes and explicit x per run, as in AImeter's generators, so
the SVG survives GitHub's image proxy and columns line up in any font.
"""
import datetime as dt
import http.server
import os
import pathlib
import re
import shutil
import socket
import subprocess
import sys
import tempfile
import threading
import time
from xml.sax.saxutils import escape

sys.dont_write_bytecode = True  # no tools/__pycache__ in the worktree
import term  # noqa: E402

ROOT = pathlib.Path(__file__).resolve().parent.parent
FIXTURE = ROOT / "tools" / "demo-board"
OUT = ROOT / "docs" / "images"
ANCHOR = dt.datetime.fromisoformat("2026-11-20T12:40:00-05:00")
WEB_PORT, API_PORT = 4173, 4174  # the fixture's running services: one up, one down
SESSION = "demo"  # the board directory's name: the viewer titles its window `board: demo`

MONO = "ui-monospace, SFMono-Regular, Menlo, Consolas, 'DejaVu Sans Mono', monospace"
EMOJI = "'Apple Color Emoji', 'Segoe UI Emoji', 'Noto Color Emoji', sans-serif"
BG, FG, CHROME, EDGE, HAIR = "#17181c", "#c3c7cd", "#1e2027", "#2a2c33", "#4a4e58"
LABEL, HEAD, FAINT = "#9aa0aa", "#6c7280", "#727783"
PALETTE = [
    "#2a2c33", "#d75f5f", "#5faf5f", "#d7af5f", "#5f87af", "#af87d7", "#5fafaf", "#c3c7cd",
    "#626672", "#ff8787", "#87d787", "#ffd787", "#87afd7", "#d7afff", "#87d7d7", "#e6e8eb",
]


# ── the board, at a fixed age ────────────────────────────────────────────────
STAMP = re.compile(r'"(\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d(?:Z|[+-]\d\d:\d\d))"')
TIMED = ("board.json", "agents.jsonl", "log.jsonl")  # decks keep their text as written


def prepare(dst, now):
    """Copy the fixture to dst, shifted so `now` is its anchor plus < 1 minute."""
    delta = dt.timedelta(minutes=(now - ANCHOR) // dt.timedelta(minutes=1))

    def shift(m):
        t = dt.datetime.fromisoformat(m.group(1).replace("Z", "+00:00"))
        mins = int((t.utcoffset() - delta).total_seconds() // 60)
        offset = dt.timedelta(minutes=(mins + 720) % 1440 - 720)  # same wall clock, mod a day
        return '"' + (t + delta).astimezone(dt.timezone(offset)).isoformat() + '"'

    shutil.copytree(FIXTURE, dst)
    for name in TIMED:
        p = dst / name
        p.write_text(STAMP.sub(shift, p.read_text()))
    mtime = now.timestamp() - 180  # `↻ 3m ago`
    for p in [dst, *dst.rglob("*")]:
        os.utime(p, (mtime, mtime))


def serve_web():
    """Answer the fixture's `web` url so the probe shows it up."""
    class Ok(http.server.BaseHTTPRequestHandler):
        def do_HEAD(self):
            self.send_response(200)
            self.end_headers()

        do_GET = do_HEAD

        def log_message(self, *_):
            pass

    with socket.socket() as s:
        if s.connect_ex(("127.0.0.1", API_PORT)) == 0:
            raise SystemExit(f"port {API_PORT} is in use; the fixture's api must be down")
    srv = http.server.ThreadingHTTPServer(("127.0.0.1", WEB_PORT), Ok)
    threading.Thread(target=srv.serve_forever, daemon=True).start()
    return srv


def shoot(binary, work, cols, rows, keys, once_args):
    """Capture the live viewer and check it against `--once` at the same instant."""
    for _ in range(3):
        now = dt.datetime.now(dt.timezone.utc)
        if (now - ANCHOR).seconds % 60 > 40:  # finish inside the anchor's minute
            time.sleep(62 - (now - ANCHOR).seconds % 60)
            continue
        board = work / f"{len(list(work.iterdir()))}" / SESSION
        board.parent.mkdir()
        prepare(board, now)
        env = {k: v for k, v in os.environ.items() if k not in ("COLORTERM", "NO_COLOR")}
        env["TERM"] = "xterm-256color"
        screen = term.capture([str(binary), str(board)], cols, rows, keys, env)
        at = dt.datetime.fromtimestamp(int(screen.at), dt.timezone.utc)
        once = subprocess.run(
            [str(binary), "--once", "--width", str(cols), "--now",
             at.strftime("%Y-%m-%dT%H:%M:%SZ"), *once_args, str(board)],
            capture_output=True, text=True, check=True,
        ).stdout.splitlines()
        if (now - ANCHOR).seconds % 60 + (at - now).seconds < 60:
            return screen, once
    raise SystemExit("could not capture inside one minute")


def check(name, screen, once, footer=1, deck=False):
    """Refuse a frame whose text is not the renderer's `--once` text.

    With deck, `--once --deck` holds the fixed area and then the deck region
    from its counter line; the live viewer has the board's body and a rule
    between the two, so the region is compared from the live counter row on.
    """
    live = [screen.text(r) for r in range(screen.rows)]
    pairs = [(i, i) for i in range(screen.rows - footer)]
    if deck:
        at = next((r for r in range(6, screen.rows) if live[r].startswith("Deck: ")), None)
        if at is None:
            raise SystemExit(f"{name}: no `Deck: ` counter row in the live frame")
        pairs = [(i, i) for i in range(6)] + [(r, 6 + r - at) for r in range(at, screen.rows - footer)]
    bad = [
        f"  row {i}: live {live[i]!r}\n         once {once[j].rstrip()!r}"
        for i, j in pairs
        if j < len(once) and live[i] != once[j].rstrip()
    ]
    if bad:
        raise SystemExit(f"{name}: live frame differs from --once\n" + "\n".join(bad))


# ── cells to SVG ─────────────────────────────────────────────────────────────
def rgb(c):
    if c[0] == "rgb":
        return "#%02x%02x%02x" % c[1:]
    n = c[1]
    if n < 16:
        return PALETTE[n]
    if n < 232:
        n -= 16
        lv = [0, 95, 135, 175, 215, 255]
        return "#%02x%02x%02x" % (lv[n // 36], lv[n // 6 % 6], lv[n % 6])
    g = 8 + (n - 232) * 10
    return "#%02x%02x%02x" % (g, g, g)


def mix(a, b, t):
    pa = [int(a[i:i + 2], 16) for i in (1, 3, 5)]
    pb = [int(b[i:i + 2], 16) for i in (1, 3, 5)]
    return "#%02x%02x%02x" % tuple(round(x + (y - x) * t) for x, y in zip(pa, pb))


def colours(s):
    fg = rgb(s.fg) if s.fg else FG
    bg = rgb(s.bg) if s.bg else None
    if s.reverse:
        fg, bg = bg or BG, fg
    if s.faint:
        fg = mix(fg, bg or BG, 0.45)
    return fg, bg


BLOCKS = {"█": 1.0, "▉": 7 / 8, "▊": 6 / 8, "▋": 5 / 8, "▌": 4 / 8, "▍": 3 / 8, "▎": 2 / 8, "▏": 1 / 8}


def draw_grid(screen, x0, y0, size, lh):
    """SVG elements for the screen's cells; the top of row 0 at y0."""
    adv = size * 0.6
    out = []
    for r in range(screen.rows):
        row = screen.grid[r]
        top = y0 + r * lh
        base = top + lh * 0.74
        # backgrounds first, merged into runs
        c = 0
        while c < screen.cols:
            bg = colours(row[c].style)[1]
            start = c
            while c < screen.cols and colours(row[c].style)[1] == bg:
                c += 1
            if bg:
                out.append(f'<rect x="{x0 + start * adv:.1f}" y="{top:.1f}" '
                           f'width="{(c - start) * adv:.1f}" height="{lh:.1f}" fill="{bg}"/>')
        c = 0
        while c < screen.cols:
            cell = row[c]
            if cell.width == 0:
                c += 1
                continue
            fg = colours(cell.style)[0]
            weight = ' font-weight="bold"' if cell.style.bold else ""
            if cell.width == 2:
                out.append(f'<text x="{x0 + c * adv:.1f}" y="{base:.1f}" font-family="{EMOJI}" '
                           f'font-size="{size * 0.92:.1f}" fill="{fg}">{escape(cell.ch)}</text>')
                c += 2
                continue
            if cell.ch in BLOCKS:  # bar cells as boxes: glyphs leave seams between them
                start, key, width = c, cell.style.key(), 0.0
                while c < screen.cols and row[c].ch in BLOCKS and row[c].style.key() == key:
                    width += BLOCKS[row[c].ch]
                    c += 1
                out.append(f'<rect x="{x0 + start * adv:.1f}" y="{top + lh * 0.14:.1f}" '
                           f'width="{width * adv:.1f}" height="{lh * 0.72:.1f}" fill="{fg}"/>')
                continue
            start, key, s = c, cell.style.key(), ""
            while (c < screen.cols and row[c].width == 1 and row[c].style.key() == key
                   and row[c].ch not in BLOCKS):
                s += row[c].ch
                c += 1
            if s.strip():
                lead = len(s) - len(s.lstrip(" "))
                body = s.strip(" ")
                x = x0 + (start + lead) * adv
                out.append(f'<text x="{x:.1f}" y="{base:.1f}" font-family="{MONO}" '
                           f'font-size="{size}" textLength="{len(body) * adv:.1f}" '
                           f'lengthAdjust="spacingAndGlyphs" fill="{fg}"{weight} '
                           f'xml:space="preserve">{escape(body)}</text>')
                if cell.style.strike:
                    y = base - size * 0.3
                    out.append(f'<line x1="{x:.1f}" y1="{y:.1f}" x2="{x + len(body) * adv:.1f}" '
                               f'y2="{y:.1f}" stroke="{fg}"/>')
    return out


def svg(w, h, label, body):
    return (f'<svg xmlns="http://www.w3.org/2000/svg" width="{w:.0f}" height="{h:.0f}" '
            f'viewBox="0 0 {w:.0f} {h:.0f}" role="img" aria-label="{escape(label)}">'
            + "".join(body) + "</svg>\n")


def window(screen, label, size=13.0):
    """The screen inside a plain terminal window, titled as the viewer titles it."""
    adv, lh, pad, bar = size * 0.6, size * 1.36, 18, 34
    w, h = screen.cols * adv + 2 * pad, bar + screen.rows * lh + 2 * pad - 6
    body = [
        f'<rect x="0" y="0" width="{w:.0f}" height="{h:.0f}" rx="8" fill="{BG}"/>',
        f'<path d="M0 8a8 8 0 0 1 8-8h{w - 16:.0f}a8 8 0 0 1 8 8v{bar - 8}H0z" fill="{CHROME}"/>',
        f'<rect x="0.5" y="0.5" width="{w - 1:.0f}" height="{h - 1:.0f}" rx="8" fill="none" stroke="{EDGE}"/>',
        f'<line x1="0" y1="{bar}" x2="{w:.0f}" y2="{bar}" stroke="{EDGE}"/>',
    ]
    for i, colour in enumerate(("#d75f5f", "#d7af5f", "#5faf5f")):
        body.append(f'<circle cx="{pad + 6 + i * 18}" cy="{bar / 2}" r="5.5" fill="{colour}" opacity="0.55"/>')
    body.append(f'<text x="{w / 2:.0f}" y="{bar / 2 + 4.5}" font-family="{MONO}" font-size="12.5" '
                f'text-anchor="middle" fill="{FAINT}">{escape(screen.title)}</text>')
    body += draw_grid(screen, pad, bar + pad - 6, size, lh)
    return svg(w, h, label, body)


# ── the annotated board ──────────────────────────────────────────────────────
def regions(screen):
    """(first row, last row, name, what it shows), found in the captured text."""
    t = [screen.text(r) for r in range(screen.rows)]
    out = [(0, 0, "Title line", ["status, title, ⏱ elapsed, ↻ last write"]),
           (1, 1, "Status line", ["progress, then counts: on you, failed,", "running, external, errors"]),
           (2, 4, "Slots", ["what needs you first: decks,", "failures, waits, agents"])]
    heads = {
        "Plan": ["finished steps fold into one line,", "the active step is highlighted"],
        "PRs": ["CI and review state of each PR"],
        "Running": ["services the task started,", "probed up or down"],
        "Details": ["sections: Markdown, charts,", "tables and checks"],
        "Log": ["the newest commits, notes and links"],
    }
    for name, what in heads.items():
        r0 = next(r for r, s in enumerate(t) if re.match(rf"^\S+ {name} ─", s))
        r1 = r0
        while r1 + 1 < screen.rows - 1 and t[r1 + 1] and not t[r1 + 1].startswith("▶️ active"):
            r1 += 1
        out.append((r0, r1, name, what))
    legend = next(r for r, s in enumerate(t) if s.startswith("▶️ active"))
    out.append((legend, legend, "Legend (g)", ["the plan markers, on demand"]))
    out.append((screen.rows - 1, screen.rows - 1, "Footer", ["every key, darker when idle"]))
    return out


def annotated(screen, label, size=12.5):
    adv, lh, pad = size * 0.6, size * 1.36, 24
    top = 64
    gx = pad + screen.cols * adv           # right edge of the board
    bx, lx = gx + 14, gx + 46              # bracket, then labels
    w = lx + 300
    h = top + screen.rows * lh + 44
    body = [
        f'<rect x="0" y="0" width="{w:.0f}" height="{h:.0f}" rx="7" fill="{BG}"/>',
        f'<rect x="0.5" y="0.5" width="{w - 1:.0f}" height="{h - 1:.0f}" rx="7" fill="none" stroke="{EDGE}"/>',
        f'<text x="{pad}" y="36" font-family="{MONO}" font-size="12.5" fill="{HEAD}">'
        f'ai-board — one session\'s board: what needs you on top, then the work</text>',
        f'<line x1="{pad}" y1="48" x2="{w - pad:.0f}" y2="48" stroke="{EDGE}"/>',
    ]
    body += draw_grid(screen, pad, top, size, lh)
    placed = top - 40
    for r0, r1, name, what in regions(screen):
        y0, y1 = top + r0 * lh + 3, top + (r1 + 1) * lh - 3
        mid = (y0 + y1) / 2
        ty = max(mid - 4, placed + 22)      # push down past the previous label
        placed = ty + 1 + 14 * len(what)    # baseline of its last line
        body.append(f'<path d="M{bx} {y0:.1f}h6V{y1:.1f}h-6" fill="none" stroke="{HAIR}"/>')
        body.append(f'<path d="M{bx + 6} {mid:.1f}L{lx - 8} {ty - 4:.1f}" fill="none" stroke="{HAIR}"/>')
        body.append(f'<text x="{lx}" y="{ty:.1f}" font-family="{MONO}" font-size="12.5" '
                    f'fill="{FG}" font-weight="bold">{escape(name)}</text>')
        for i, line in enumerate(what):
            body.append(f'<text x="{lx}" y="{ty + 15 + 14 * i:.1f}" font-family="{MONO}" '
                        f'font-size="11" fill="{LABEL}">{escape(line)}</text>')
    return svg(w, h, label, body)


# ── the three shots ──────────────────────────────────────────────────────────
def main():
    dump = "--dump" in sys.argv
    OUT.mkdir(parents=True, exist_ok=True)
    srv = serve_web()
    with tempfile.TemporaryDirectory() as tmp:
        work = pathlib.Path(tmp)
        binary = work / "ai-board"
        subprocess.run(["go", "build", "-o", str(binary), "./cmd/ai-board"], cwd=ROOT, check=True)
        (work / "boards").mkdir()
        boards = work / "boards"

        full = len(subprocess.run(
            [str(binary), "--once", "--width", "100", "--now", ANCHOR.isoformat(), str(FIXTURE)],
            capture_output=True, text=True, check=True).stdout.splitlines())

        shots = [
            # name, cols, rows, keys, --once args, footer lines
            ("console", 100, 30, "", [], 1),
            ("board", 100, full + 2, "g", [], 2),  # every block, the legend line and the footer
            ("deck", 100, 34, "D", ["--deck", "export-format"], 3),  # the nav line and the footer
        ]
        screens = {}
        for name, cols, rows, keys, args, footer in shots:
            screen, once = shoot(binary, boards, cols, rows, keys, args)
            if dump:
                print(f"── {name} {cols}x{rows} {screen.title!r}")
                print("\n".join(screen.text(r) for r in range(rows)))
            check(name, screen, once, footer, deck=name == "deck")
            screens[name] = screen

    srv.shutdown()
    files = {
        "console.svg": window(screens["console"],
            "The ai-board viewer in a terminal: a board titled CSV export for orders, waiting on you, "
            "with a deck to answer, a CI wait and running agents in the header, then the plan with "
            "finished steps folded into one line and the active step highlighted, two PRs with CI and "
            "review state, and two local services, one up and one down."),
        "board.svg": annotated(screens["board"],
            "The whole board, annotated: the title line with elapsed time and last write, the status "
            "line with progress and counts, three slots for what needs you, then the Plan, PRs, "
            "Running, Details and Log blocks, the legend of plan markers and the footer of keys."),
        "deck.svg": window(screens["deck"],
            "The deck view under the board's plan: decision 1 of 2 of a deck titled Export format, "
            "with option A picked and marked recommended with its pros, cons and why, option B with "
            "a note on it, Other and Suggest another option, then the Back and Next buttons, the "
            "Send button and the deck keys."),
    }
    for fname, text in files.items():
        (OUT / fname).write_text(text)
        print(f"wrote {OUT / fname}")


if __name__ == "__main__":
    main()
