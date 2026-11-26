package board

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// legendItems are the plan markers the legend line names, in order.
var legendItems = []struct{ sym, label string }{
	{"▶️", "active"},
	{"📥", "on you"},
	{"⏳", "external"},
	{"❌", "failed"},
	{"⚪", "todo"},
	{"✅", "done"},
}

// LegendLine is the fixed dim legend line, `▶️ active · 📥 on you · …`.
// Entries that do not fit in width are dropped from the end, leaving a
// trailing `…`.
func (r *Renderer) LegendLine(width int) string {
	r.init()
	const sep = " · "
	var out string
	for _, it := range legendItems {
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

// RenderHelp is the full-screen `h` help: every marker, icon, colour and key.
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
		lines = append(lines, trunc(pad(sym, 7, false)+text, width))
	}
	lines = append(lines, r.heading("❔", "Help", width))

	sub("Plan steps")
	item("▶️", "active step, its row highlighted, ⏱ elapsed on the right")
	item("📥", "waiting on you")
	item("⏳", "external: CI, an approval, a deploy")
	item("❌", "failed")
	item("⚪", "todo")
	item("✅", "done, with its duration; struck through when skipped")
	item("✅ N", "more than three finished steps, collapsed (· last: the latest)")

	sub("Status")
	item("🔵", "working")
	item("🟡", "waiting on you")
	item("🔴", "blocked")
	item("🟢", "done: the status line is a green band, bar full")
	item("⚪", "no status")

	sub("Header")
	item("⏱", "elapsed since the first step started")
	item("↻", "updated: time since the last write")
	item("⚠️", "stale (working, no write for 10 min) or a file that failed to parse")
	item("📥N", "status line counts: 📥 on you, ❌ failures (agents, steps), 🤖 running, ⏳ external, ⚠️ errors")
	item("📥", "pending on you; ↗ when it has a link (the text opens it)")
	item("❌", "failed agent, with the time it stopped")
	item("❌", "failed step, with its note")
	item("⏳", "external wait: who is awaited, time since it started, ↗ link")
	item("🤖", "running agent: elapsed, or a bar of elapsed against expected")
	item("+N", "more items than the three rows hold (a: every agent)")
	item(r.st.off.Render("📥"), "darker: a zero count or a key that does nothing now")

	sub("Checks")
	item("✅", "all checks pass")
	item("❌", "checks failed; only the failing ones are listed")

	sub("PRs")
	item("🔀", "repo#n title; the text opens the PR (↗ when it has a link)")
	item("draft", "state word: draft, merged or closed (dim); open shows none")
	item("✅❌⏳", "CI passed, failed, pending")
	item("👍👎👀", "review approved, changes requested, pending")

	sub("Running")
	item("🚀", "a service the task started: name, url (the url opens it), login hint")
	item("✅❌", "up or down at the last probe (every 30 s); ? not probed yet")

	sub("Decks")
	item(DecisionIcon, "Deck: title · answered/total: a deck waiting for your answers (it opens by itself; D after esc)")
	item("💬", "Deck: title · Claude: the session's reply to the answers you sent")
	item("● ○", "picked, not picked; ★ recommended, with why: the reason")
	item("✓ 1A", "the item's pick (✓ 1 Other for Other); open when it has none")
	item("✎", "the item's note (n), the general note (N), under an option the note on it (e)")
	item("›", "the focused option (← →); + note writes a note on it")
	item("Generated", "an option Claude proposed with Suggest (g); ✦ Thinking… while it runs")

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
	item("a", "list every agent in the body (when one is not in the slots)")
	item("c", "list every check or only the failing ones")
	item("l", "full-screen log")
	item("g", "show or hide the legend line")
	item("h", "this help")
	item("w", "narrow footer: keys only")
	item("D", "open the deck (a list first when there are several)")
	item("q", "quit")

	sub("Deck keys")
	item("⏎ ⇥", "next decision; after the last one the review, where ⏎ sends")
	item("⌫ ⇧⇥", "previous decision")
	item("j k", "scroll the decision (also ↑ ↓, pgup pgdn and the wheel)")
	item("a-d 1-9", "pick that option; the same key again clears it; x clears")
	item("y r f", "on an Approve / Reject / Defer item: approve, reject, defer")
	item("O", "answer with Other and write what you want instead")
	item("← →", "focus an option of the item")
	item("e", "edit the note on the focused option (else the picked one), picked or not")
	item("g", "Suggest: ask Claude for one more option (claude -p)")
	item("R", "fill every open item with its recommendation (never replaces a pick)")
	item("u", "jump to the next decision without a pick")
	item("n N", "edit the item note, the general note (enter saves, alt+enter newline, esc cancels)")
	item("o", "open the item's link, else the deck's")
	item("s", "send: write the answers and copy them (asks first when items are open)")
	item("esc D", "back to the board")
	item("click", "an option picks it, + note or ✎ edits a note, Other, Suggest, Back, Next, a review row, [Send] sends")
	return strings.Join(lines, "\n")
}
