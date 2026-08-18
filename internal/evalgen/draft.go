package evalgen

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/MithrilBytes/overwater/rules"
)

// Prompt drafting: seed a prompts.jsonl from string literals near the
// call site, shaped the way the call site is. Local reads only.

var reLiteral = regexp.MustCompile("`[^`]{20,600}`|\"[^\"\\n]{20,600}\"|'[^'\\n]{20,600}'")

// The script reproduces the call site's shape from the row, so a
// drafted row carries the two values a literal read can recover
// honestly. A response schema or a tool list cannot be rebuilt from a
// window of text, and a guess there would be worse than the omission.
var (
	reDraftMaxTokens   = regexp.MustCompile(`(?i)["']?max_?(?:output_?|completion_?)?tokens["']?\s*[:=]\s*([0-9][0-9_]*)`)
	reDraftTemperature = regexp.MustCompile(`(?i)["']?temperature["']?\s*[:=]\s*([0-9]*\.?[0-9]+)`)
)

const (
	draftWindowLines = 40
	draftMaxPrompts  = 10
	// The shape belongs to the call, not to its neighborhood, so it is
	// read from a tighter span than the prompts are: at forty lines the
	// cap of the next call down reads as this one's.
	draftShapeLines = 6
)

// draftPrompts pulls distinct string literals from a window around the
// call line, in source order, capped.
func draftPrompts(content string, line int) []string {
	lines := strings.Split(content, "\n")
	start := max(0, line-1-draftWindowLines)
	end := min(len(lines), line+draftWindowLines)
	window := strings.Join(lines[start:end], "\n")

	seen := map[string]bool{}
	var prompts []string
	for _, match := range reLiteral.FindAllString(window, -1) {
		text := strings.TrimSpace(match[1 : len(match)-1])
		if len(text) < 20 || seen[text] {
			continue
		}
		seen[text] = true
		prompts = append(prompts, text)
		if len(prompts) == draftMaxPrompts {
			break
		}
	}
	return prompts
}

// draftShape pulls the call parameters the generated script replays
// from a row: the site's own output cap, and its temperature under the
// params key the script hands to the SDK verbatim.
func draftShape(content string, line int) map[string]any {
	lines := strings.Split(content, "\n")
	start := max(0, line-1-draftShapeLines)
	end := min(len(lines), line+draftShapeLines)
	window := strings.Join(lines[start:end], "\n")

	shape := map[string]any{}
	if m := reDraftMaxTokens.FindStringSubmatch(window); m != nil {
		if v, err := strconv.Atoi(strings.ReplaceAll(m[1], "_", "")); err == nil && v > 0 {
			shape["max_tokens"] = v
		}
	}
	if m := reDraftTemperature.FindStringSubmatch(window); m != nil {
		if v, err := strconv.ParseFloat(m[1], 64); err == nil {
			shape["params"] = map[string]any{"temperature": v}
		}
	}
	return shape
}

// DraftPromptSets writes one <script>.prompts.jsonl per finding that got
// an eval script, drafted from literals near its call site. Unreadable
// files and files with no literals are skipped silently.
func DraftPromptSets(root string, findings []rules.Finding, dir string) ([]string, error) {
	var written []string
	for _, f := range findings {
		if f.CandidateModel == "" {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(f.File)))
		if err != nil {
			continue
		}
		prompts := draftPrompts(string(raw), f.Line)
		if len(prompts) == 0 {
			continue
		}
		shape := draftShape(string(raw), f.Line)
		var b strings.Builder
		for _, p := range prompts {
			row := map[string]any{"prompt": p}
			for k, v := range shape {
				row[k] = v
			}
			line, err := json.Marshal(row)
			if err != nil {
				return written, err
			}
			b.Write(line)
			b.WriteByte('\n')
		}
		name := strings.TrimSuffix(scriptName(f), ".py") + ".prompts.jsonl"
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
			return written, err
		}
		written = append(written, path)
	}
	return written, nil
}
