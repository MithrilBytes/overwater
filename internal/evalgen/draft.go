package evalgen

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/MithrilBytes/overwater/rules"
)

// Prompt drafting: seed a prompts.jsonl from string literals near the
// call site. Local reads only.

var reLiteral = regexp.MustCompile("`[^`]{20,600}`|\"[^\"\\n]{20,600}\"|'[^'\\n]{20,600}'")

const (
	draftWindowLines = 40
	draftMaxPrompts  = 10
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
		var b strings.Builder
		for _, p := range prompts {
			line, err := json.Marshal(map[string]string{"prompt": p})
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
