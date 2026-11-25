"""Run the real ai-board viewer in a pseudo-terminal and read back its screen.

The README images are drawn from this screen, not from a description of it:
`capture()` starts the built binary in a pty of a fixed size, answers the two
terminal queries it makes at startup (background colour and cursor position),
presses the keys it is given and replays every byte it printed through a small
VT emulator. What comes back is a grid of cells, each with its character and
its SGR style, exactly as a terminal of that size would hold it.

Standard library only.
"""
import codecs
import fcntl
import os
import pty
import select
import signal
import struct
import termios
import time
import unicodedata


class Style:
    __slots__ = ("fg", "bg", "bold", "faint", "italic", "underline", "reverse", "strike")

    def __init__(self):
        self.fg = self.bg = None  # None, ("i", 0-255) or ("rgb", r, g, b)
        self.bold = self.faint = self.italic = self.underline = self.reverse = self.strike = False

    def copy(self):
        s = Style()
        for k in self.__slots__:
            setattr(s, k, getattr(self, k))
        return s

    def key(self):
        return tuple(getattr(self, k) for k in self.__slots__)


class Cell:
    __slots__ = ("ch", "width", "style")

    def __init__(self, ch=" ", width=1, style=None):
        self.ch, self.width, self.style = ch, width, style or Style()


def char_width(ch):
    """Cells a character takes, as the renderer counts them (uniseg-style)."""
    if unicodedata.combining(ch) or ch in "‍︎️":
        return 0
    if unicodedata.east_asian_width(ch) in "WF" or 0x1F000 <= ord(ch) <= 0x1FAFF:
        return 2
    return 1


class Screen:
    def __init__(self, cols, rows):
        self.cols, self.rows = cols, rows
        self.title = ""
        self.style = Style()
        self.clear()
        self.decoder = codecs.getincrementaldecoder("utf-8")("replace")
        self.pending = ""  # an unfinished escape sequence

    def clear(self):
        self.grid = [[Cell() for _ in range(self.cols)] for _ in range(self.rows)]
        self.r = self.c = 0
        self.wrap = False
        self.last = None  # (row, col) of the last printed cell, for FE0F and ZWJ

    def feed(self, data):
        text = self.pending + self.decoder.decode(data)
        self.pending = ""
        i, n = 0, len(text)
        while i < n:
            ch = text[i]
            if ch == "\x1b":
                end = self._escape(text, i)
                if end is None:
                    self.pending = text[i:]
                    return
                i = end
                continue
            if ch == "\r":
                self.c, self.wrap = 0, False
            elif ch == "\n":
                self._down()
            elif ch == "\b":
                self.c = max(0, self.c - 1)
            elif ch >= " ":
                self._print(ch)
            i += 1

    # ── printing ────────────────────────────────────────────────────────────
    def _down(self):
        self.wrap = False
        if self.r == self.rows - 1:
            self.grid.pop(0)
            self.grid.append([Cell() for _ in range(self.cols)])
        else:
            self.r += 1

    def _print(self, ch):
        w = char_width(ch)
        if w == 0:
            if self.last:
                r, c = self.last
                cell = self.grid[r][c]
                cell.ch += ch
                if ch == "️" and cell.width == 1:  # emoji presentation: two cells
                    cell.width = 2
                    if c + 1 < self.cols:
                        self.grid[r][c + 1] = Cell("", 0, cell.style)
                    if c + 2 >= self.cols:
                        self.c, self.wrap = self.cols - 1, True
                    else:
                        self.c = c + 2
            return
        if self.last and self.grid[self.last[0]][self.last[1]].ch.endswith("‍"):
            self.grid[self.last[0]][self.last[1]].ch += ch  # joined emoji: same cluster
            return
        if self.wrap or self.c + w > self.cols:
            self.c = 0
            self._down()
        style = self.style.copy()
        self.grid[self.r][self.c] = Cell(ch, w, style)
        if w == 2 and self.c + 1 < self.cols:
            self.grid[self.r][self.c + 1] = Cell("", 0, style)
        self.last = (self.r, self.c)
        if self.c + w >= self.cols:
            self.c, self.wrap = self.cols - 1, True
        else:
            self.c += w

    # ── escapes ─────────────────────────────────────────────────────────────
    def _escape(self, text, i):
        """Apply the sequence at text[i]; return the index after it, None if cut."""
        if i + 1 >= len(text):
            return None
        kind = text[i + 1]
        if kind == "[":
            j = i + 2
            while j < len(text) and not ("\x40" <= text[j] <= "\x7e"):
                j += 1
            if j >= len(text):
                return None
            self._csi(text[i + 2:j], text[j])
            return j + 1
        if kind == "]":
            j = i + 2
            while j < len(text):
                if text[j] == "\x07":
                    body, end = text[i + 2:j], j + 1
                    break
                if text[j] == "\x1b" and j + 1 < len(text) and text[j + 1] == "\\":
                    body, end = text[i + 2:j], j + 2
                    break
                j += 1
            else:
                return None
            if body[:2] in ("0;", "2;"):
                self.title = body[2:]
            return end  # OSC 8 hyperlinks and the rest change no cell
        if kind in "()":
            return i + 3
        if kind == "7":
            self.saved = (self.r, self.c)
        elif kind == "8":
            self.r, self.c = getattr(self, "saved", (0, 0))
        elif kind == "M":
            self.r = max(0, self.r - 1)
        return i + 2

    def _csi(self, params, final):
        if params.startswith("?"):
            if final in "hl" and any(p in ("1049", "47", "1047") for p in params[1:].split(";")):
                self.clear()  # alternate screen in or out: a blank page
            return
        if params.startswith((">", "=", "<")):
            return
        args = [int(p) if p.isdigit() else 0 for p in params.split(";")] if params else []
        a = args[0] if args else 0
        n = a or 1
        if final == "m":
            self._sgr(args or [0])
            return
        self.wrap = False
        if final == "A":
            self.r = max(0, self.r - n)
        elif final == "B":
            self.r = min(self.rows - 1, self.r + n)
        elif final == "C":
            self.c = min(self.cols - 1, self.c + n)
        elif final == "D":
            self.c = max(0, self.c - n)
        elif final == "E":
            self.r, self.c = min(self.rows - 1, self.r + n), 0
        elif final == "F":
            self.r, self.c = max(0, self.r - n), 0
        elif final == "G":
            self.c = min(self.cols - 1, n - 1)
        elif final == "d":
            self.r = min(self.rows - 1, n - 1)
        elif final in "Hf":
            row = args[0] if args else 1
            col = args[1] if len(args) > 1 else 1
            self.r, self.c = min(self.rows - 1, max(row, 1) - 1), min(self.cols - 1, max(col, 1) - 1)
        elif final == "K":
            lo, hi = {0: (self.c, self.cols), 1: (0, self.c + 1), 2: (0, self.cols)}.get(a, (0, 0))
            for c in range(lo, hi):
                self.grid[self.r][c] = Cell()
        elif final == "J":
            rows = {0: range(self.r + 1, self.rows), 1: range(0, self.r), 2: range(self.rows)}.get(a, ())
            if a == 0:
                for c in range(self.c, self.cols):
                    self.grid[self.r][c] = Cell()
            for r in rows:
                self.grid[r] = [Cell() for _ in range(self.cols)]
        elif final == "X":
            for c in range(self.c, min(self.cols, self.c + n)):
                self.grid[self.r][c] = Cell()
        elif final == "L":
            for _ in range(n):
                self.grid.insert(self.r, [Cell() for _ in range(self.cols)])
                self.grid.pop()
        elif final == "M":
            for _ in range(n):
                self.grid.pop(self.r)
                self.grid.append([Cell() for _ in range(self.cols)])

    def _sgr(self, args):
        s, i = self.style, 0
        while i < len(args):
            a = args[i]
            if a == 0:
                self.style = s = Style()
            elif a == 1:
                s.bold = True
            elif a == 2:
                s.faint = True
            elif a == 3:
                s.italic = True
            elif a == 4:
                s.underline = True
            elif a == 7:
                s.reverse = True
            elif a == 9:
                s.strike = True
            elif a == 22:
                s.bold = s.faint = False
            elif a == 23:
                s.italic = False
            elif a == 24:
                s.underline = False
            elif a == 27:
                s.reverse = False
            elif a == 29:
                s.strike = False
            elif 30 <= a <= 37:
                s.fg = ("i", a - 30)
            elif 90 <= a <= 97:
                s.fg = ("i", a - 90 + 8)
            elif 40 <= a <= 47:
                s.bg = ("i", a - 40)
            elif 100 <= a <= 107:
                s.bg = ("i", a - 100 + 8)
            elif a == 39:
                s.fg = None
            elif a == 49:
                s.bg = None
            elif a in (38, 48) and i + 1 < len(args):
                if args[i + 1] == 5 and i + 2 < len(args):
                    colour, i = ("i", args[i + 2]), i + 2
                elif args[i + 1] == 2 and i + 4 < len(args):
                    colour, i = ("rgb", *args[i + 2:i + 5]), i + 4
                else:
                    colour = None
                if a == 38:
                    s.fg = colour
                else:
                    s.bg = colour
            i += 1

    def text(self, r):
        """Row r as plain text, trailing blanks removed."""
        return "".join(c.ch for c in self.grid[r] if c.width).rstrip()


def capture(argv, cols, rows, keys=(), env=None, settle=2.5):
    """Run argv in a cols x rows pty, press keys, return the Screen."""
    pid, fd = pty.fork()
    if pid == 0:
        fcntl.ioctl(0, termios.TIOCSWINSZ, struct.pack("HHHH", rows, cols, 0, 0))
        os.execve(argv[0], argv, env or os.environ)
    screen = Screen(cols, rows)

    def pump(seconds):
        deadline = time.monotonic() + seconds
        while (left := deadline - time.monotonic()) > 0:
            ready, _, _ = select.select([fd], [], [], left)
            if not ready:
                continue
            try:
                data = os.read(fd, 65536)
            except OSError:
                return
            if not data:
                return
            if b"]11;?" in data:
                os.write(fd, b"\x1b]11;rgb:1717/1818/1c1c\x1b\\")
            if b"]10;?" in data:
                os.write(fd, b"\x1b]10;rgb:c3c3/c7c7/cdcd\x1b\\")
            if b"\x1b[6n" in data:
                os.write(fd, b"\x1b[1;1R")
            screen.feed(data)

    pump(settle)
    for k in keys:
        os.write(fd, k.encode())
        pump(1.0)
    snap = [[Cell(c.ch, c.width, c.style.copy()) for c in row] for row in screen.grid]
    title, at = screen.title, time.time()
    os.write(fd, b"q")
    pump(0.5)
    try:
        os.kill(pid, signal.SIGTERM)
    except ProcessLookupError:
        pass
    os.waitpid(pid, 0)
    os.close(fd)
    screen.grid, screen.title, screen.at = snap, title, at
    return screen
