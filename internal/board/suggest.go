package board

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// suggestTimeout bounds one `claude -p` call for Suggest.
const suggestTimeout = 2 * time.Minute

// runClaude runs `claude -p` on prompt (given on stdin, so the variadic
// --tools cannot swallow it) and returns its stdout, the JSON result
// envelope. Tests replace it.
var runClaude = func(ctx context.Context, prompt string) ([]byte, error) {
	c := exec.CommandContext(ctx, "claude", "-p", "--output-format", "json", "--no-session-persistence", "--tools", "")
	c.Stdin = strings.NewReader(prompt)
	return c.Output()
}

// suggestPrompt is what the web template sends for "Suggest another
// option": its context() for the item, then the instruction.
func suggestPrompt(d *Deck, it DeckItem, s DeckState) string {
	var b strings.Builder
	b.WriteString("Deck: " + d.Title)
	if d.Project != "" {
		b.WriteString(" (project: " + d.Project + ")")
	}
	fmt.Fprintf(&b, "\nItem %d: %s\n%s\nOptions:", it.N, it.Title, it.Summary)
	for _, o := range OptionsFor(it, s) {
		b.WriteString("\n" + o.Key + ". " + o.Title + " — " + o.Summary)
	}
	b.WriteString("\n\nPropose ONE new option that differs from all of the above. Reply as JSON: " +
		`{"title": string (max 6 words), "summary": string (one sentence), "pros": string[], "cons": string[], "effort": "S"|"M"|"L", "risk": "low"|"med"|"high"}`)
	return b.String()
}

// suggestErr is a failed Suggest, with the code the footer shows.
type suggestErr struct{ code string }

func (e suggestErr) Error() string { return e.code }

// suggestFailText is the web template's wording for a Suggest that brought
// nothing back.
func suggestFailText(err error) string {
	code := "error"
	if e, ok := err.(suggestErr); ok {
		code = e.code
	}
	return "No option came back (" + code + "). Try again."
}

// suggestOption asks Claude for one new option for it and returns it without
// its key: the generated flag set, fields coerced like the web template.
func suggestOption(ctx context.Context, prompt string) (DeckOption, error) {
	ctx, cancel := context.WithTimeout(ctx, suggestTimeout)
	defer cancel()
	out, err := runClaude(ctx, prompt)
	if err != nil {
		return DeckOption{}, suggestErr{"error"}
	}
	return parseSuggestion(out)
}

// parseSuggestion reads claude's JSON result envelope and the option object
// in its `result` text (bare, or inside a fenced block).
func parseSuggestion(out []byte) (DeckOption, error) {
	var env struct {
		IsError bool   `json:"is_error"`
		Result  string `json:"result"`
	}
	if err := json.Unmarshal(out, &env); err != nil || env.IsError {
		return DeckOption{}, suggestErr{"error"}
	}
	i, j := strings.Index(env.Result, "{"), strings.LastIndex(env.Result, "}")
	var o map[string]any
	if i < 0 || j < i || json.Unmarshal([]byte(env.Result[i:j+1]), &o) != nil {
		return DeckOption{}, suggestErr{"bad_json"}
	}
	title, ok := o["title"].(string)
	if !ok {
		return DeckOption{}, suggestErr{"bad_shape"}
	}
	return DeckOption{Title: title, Summary: jsOr(o["summary"]), Pros: jsList(o["pros"]), Cons: jsList(o["cons"]),
		Effort: jsOr(o["effort"]), Risk: jsOr(o["risk"]), Generated: true}, nil
}

// jsString is JavaScript's String(v) for a decoded JSON value.
func jsString(v any) string {
	switch v := v.(type) {
	case nil:
		return "null"
	case string:
		return v
	case bool:
		return strconv.FormatBool(v)
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	case []any:
		parts := make([]string, len(v))
		for i, x := range v {
			if x != nil {
				parts[i] = jsString(x)
			}
		}
		return strings.Join(parts, ",")
	}
	return "[object Object]"
}

// falsy is JavaScript's !v for a decoded JSON value.
func falsy(v any) bool { return v == nil || v == false || v == "" || v == 0.0 }

// jsOr is String(v || "").
func jsOr(v any) string {
	if falsy(v) {
		return ""
	}
	return jsString(v)
}

// jsList is [].concat(v || []).map(String).
func jsList(v any) []string {
	if falsy(v) {
		return []string{}
	}
	xs, ok := v.([]any)
	if !ok {
		xs = []any{v}
	}
	out := make([]string, len(xs))
	for i, x := range xs {
		out[i] = jsString(x)
	}
	return out
}
