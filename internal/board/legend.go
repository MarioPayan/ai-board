package board

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// legendItems is every symbol the legend can name, in legend order.
var legendItems = []struct{ sym, label string }{
	{"▶", "active"},
	{"🟡", "on you"},
	{"🟣", "external"},
	{"✘", "failed"},
	{"○", "todo"},
	{"✔", "done"},
	{"⏱", "elapsed"},
	{"↻", "updated"},
	{"◆", "commit"},
	{"•", "note"},
	{"↗", "link"},
	{"📥", "pending"},
	{"🤖", "agent"},
	{"🔴", "failed/blocked"},
	{"🔵", "working"},
	{"🟢", "finished"},
	{"⚠", "stale/error"},
}

// LegendLine is one dim line naming each symbol in syms, in legend order,
// as `▶ active · ✔ done · …`. Entries that do not fit in width are dropped
// from the end, leaving a trailing `…`. It is "" when syms holds none.
func (r *Renderer) LegendLine(syms []string, width int) string {
	r.init()
	seen := map[string]bool{}
	for _, s := range syms {
		seen[s] = true
	}
	const sep = " · "
	var out string
	for _, it := range legendItems {
		if !seen[it.sym] {
			continue
		}
		entry := it.sym + " " + it.label
		next := entry
		if out != "" {
			next = out + sep + entry
		}
		if ansi.StringWidth(next) > width {
			out = trunc(out+sep+"…", width)
			break
		}
		out = next
	}
	return r.st.dim.Render(out)
}

// RenderHelp is the full-screen `?` help: every marker, icon, colour and key.
func (r *Renderer) RenderHelp(width int) string {
	r.init()
	var lines []string
	sub := func(title string) {
		if len(lines) > 1 {
			lines = append(lines, "")
		}
		lines = append(lines, r.st.bold.Render(title))
	}
	item := func(sym, text string) {
		lines = append(lines, trunc(pad(sym, 4, false)+text, width))
	}
	lines = append(lines, r.heading("❔", "Help", width))

	sub("Plan steps")
	item(r.st.accent.Render("▶"), "active step, its row highlighted, ⏱ elapsed on the right")
	item("🟡", "waiting on you")
	item("🟣", "external: CI, a subagent, a deploy")
	item(r.st.failed.Render("✘"), "failed")
	item("○", "todo")
	item("✔", "done, with its duration; struck through when skipped")
	item("✔ N", "more than three finished steps, collapsed (· last: the latest)")

	sub("Status")
	item("🔵", "working")
	item("🟡", "waiting on you")
	item("🔴", "blocked")
	item("🟢", "done: a green band, bar full")

	sub("Header, pending and agents")
	item("⏱", "elapsed since the first step started")
	item("↻", "updated: time since the last write")
	item(r.st.err.Render("⚠"), "stale (working, no write for 10 min) or a file that failed to parse")
	item(r.st.pendBar.Render("┃")+" 📥", "pending on you, with its link")
	item("🤖", "running agent: elapsed, or a bar of elapsed against expected")
	item("🔴", "failed agent, with the time it stopped")
	item("🤖 +N", "agents beyond the first three (M over: past their expected time)")

	sub("Checks")
	item(r.st.ok.Render("✔"), "all checks pass")
	item(r.st.err.Render("✘"), "checks failed; only the failing ones are listed")

	sub("Log")
	item("◆", "commit")
	item("•", "note")
	item("↗", "link")
	item("·", "other entry")

	sub("Colours")
	item(r.st.noteWaiting.Render("■"), "yellow: waiting on you, pending")
	item(r.st.err.Render("■"), "red: failed, over time, stale, errors")
	item(r.st.noteExternal.Render("■"), "magenta: external")
	item(r.st.accent.Render("■"), "cyan: the active step, inline code")
	item(r.st.ok.Render("■"), "green: checks pass, board done")
	item(r.st.bar.Render("■"), "blue: progress and time bars")
	item(r.st.dim.Render("■"), "dim: times, notes, links, finished steps")

	sub("Keys")
	item("j k", "scroll (also ↑ ↓, pgup pgdn)")
	item("⇥", "next section (⇧⇥ previous)")
	item("d", "expand or collapse finished steps")
	item("a", "list every agent or the first three")
	item("c", "list every check or only the failing ones")
	item("l", "full-screen log")
	item("?", "this help")
	item("q", "quit")
	return strings.Join(lines, "\n")
}
