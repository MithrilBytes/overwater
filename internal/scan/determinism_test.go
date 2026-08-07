package scan

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// writeTree materializes files under a fresh temp dir and returns it.
func writeTree(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		full := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// Twenty in process runs over a repo built to expose every map order
// hazard at once: duplicate normalized property keys, an ambiguous
// workspace suffix import, and two models on one line. Every report
// must be deep equal to the first.
func TestAnalyzeIsDeterministic(t *testing.T) {
	promptA := strings.Repeat("Answer briefly and cite the doc. ", 3)
	promptB := strings.Repeat("Answer with citations and disclaimers, always in full sentences. ", 3)
	dir := writeTree(t, map[string]string{
		// Both spellings of the token cap in one call: propVal must not
		// resolve by map iteration order.
		"app.ts": `import { generateText } from "ai";
import { anthropic } from "@ai-sdk/anthropic";
import { SYS } from "lib/prompts";

export async function answerThing(q: string) {
  return generateText({
    model: anthropic("claude-sonnet-5"),
    system: SYS,
    max_tokens: 300,
    maxTokens: 200,
    prompt: q,
  });
}
`,
		// Two candidates for the bare "lib/prompts" import, different
		// prompt lengths so a flipped resolution changes the shape.
		"a/lib/prompts.ts": "export const SYS = `" + promptA + "`;\n",
		"b/lib/prompts.ts": "export const SYS = `" + promptB + "`;\n",
		// Two models on one line: the final sort needs Col to order them.
		"models.ts": "export const PAIR = [\"gpt-4o\", \"gpt-4o-mini\"];\n",
	})
	cat := mustCatalog(t)

	var first *Report
	for i := 0; i < 20; i++ {
		r, err := Analyze(dir, cat)
		if err != nil {
			t.Fatal(err)
		}
		if first == nil {
			first = r
			continue
		}
		if !reflect.DeepEqual(first, r) {
			t.Fatalf("run %d differs from run 0:\nfirst: %+v\n  got: %+v", i, first.Sites, r.Sites)
		}
	}

	var app *Site
	var pair []Site
	for i := range first.Sites {
		switch first.Sites[i].File {
		case "app.ts":
			app = &first.Sites[i]
		case "models.ts":
			pair = append(pair, first.Sites[i])
		}
	}
	if app == nil {
		t.Fatalf("no app.ts site: %+v", first.Sites)
	}
	// The alias list names max_tokens first; its exact spelling wins
	// over the camelCase duplicate.
	if app.Shape.MaxTokens == nil || *app.Shape.MaxTokens != 300 {
		t.Errorf("max tokens = %v, want 300 from the max_tokens spelling", app.Shape.MaxTokens)
	}
	// Sorted candidates: a/lib/prompts.ts resolves before b/lib.
	if app.Shape.SystemPromptChars != len(promptA) {
		t.Errorf("system prompt = %d chars, want %d from a/lib/prompts.ts",
			app.Shape.SystemPromptChars, len(promptA))
	}
	if len(pair) != 2 {
		t.Fatalf("models.ts sites = %d, want 2", len(pair))
	}
	if pair[0].Line != pair[1].Line || pair[0].Col >= pair[1].Col {
		t.Errorf("same line sites out of Col order: %+v", pair)
	}
	if pair[0].ModelID != "gpt-4o" || pair[1].ModelID != "gpt-4o-mini" {
		t.Errorf("pair = %s, %s, want gpt-4o then gpt-4o-mini by column", pair[0].ModelID, pair[1].ModelID)
	}
}
