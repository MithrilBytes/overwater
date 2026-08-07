package scan

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// yaml spells config keys lowercase; model: still traces to its reader
// when the value names a known model.
func TestLowercaseConfigKeyTraces(t *testing.T) {
	r := analyzeTemp(t, map[string]string{
		"settings.yaml": "model: gpt-4o-mini\n",
		"reader.py": `import os

def route_request(text):
    return client.chat.completions.create(
        model=os.environ.get("model"),
        max_tokens=200,
        messages=[{"role": "user", "content": text}],
    )
`,
	})
	reader := siteInFile(t, r, "reader.py")
	if !reader.Known || reader.ModelID != "gpt-4o-mini" {
		t.Errorf("reader = %+v, want gpt-4o-mini traced from settings.yaml", reader)
	}
	if !strings.Contains(reader.ViaConfig, "model") {
		t.Errorf("ViaConfig = %q", reader.ViaConfig)
	}
}

// Lowercase keys only trace values the catalog knows; a private name
// under a lowercase key stays untraced so yaml prose cannot pose as
// config.
func TestLowercaseUnknownValueSkipped(t *testing.T) {
	r := analyzeTemp(t, map[string]string{
		"settings.yaml": "model: internal-router-v2\n",
		"reader.py": `import os

def route_request(text):
    return client.chat.completions.create(
        model=os.environ.get("model"),
        max_tokens=200,
        messages=[{"role": "user", "content": text}],
    )
`,
	})
	for _, s := range r.Sites {
		if s.File == "reader.py" {
			t.Errorf("unknown lowercase config value traced: %+v", s)
		}
	}
}

// Python single quoted constants resolve as system prompts, including
// escaped quotes and the triple single form.
func TestPythonSingleQuoteConst(t *testing.T) {
	prompt := "Categorize the ticket into billing, bug, or feature request. Reply with the label only."
	r := analyzeTemp(t, map[string]string{"label.py": `import anthropic

client = anthropic.Anthropic()

PROMPT = '` + prompt + `'


def label_ticket(text):
    return client.messages.create(
        model="claude-haiku-4-5",
        max_tokens=20,
        system=PROMPT,
        messages=[{"role": "user", "content": text}],
    )
`})
	s := soleSite(t, r).Shape
	if s.SystemPromptChars != len(prompt) {
		t.Errorf("system prompt = %d chars, want %d from the single quoted constant",
			s.SystemPromptChars, len(prompt))
	}
}

func TestResolveConstQuoteForms(t *testing.T) {
	if text, ok := resolveConstIn(`P = 'It\'s here'`, "P"); !ok || text != `It\'s here` {
		t.Errorf("escaped single quote resolved %q %v", text, ok)
	}
	multi := "P = '''Turn the raw notes into\nfive crisp bullets.'''\n"
	if text, ok := resolveConstIn(multi, "P"); !ok || text != "Turn the raw notes into\nfive crisp bullets." {
		t.Errorf("triple single quote resolved %q %v", text, ok)
	}
}

// tsconfig.json ships with comments in the wild; the alias must still
// resolve.
func TestTsconfigWithComments(t *testing.T) {
	guide := strings.Repeat("Answer with the runbook steps before improvising. ", 6)
	r := analyzeTemp(t, map[string]string{
		"tsconfig.json": `{
  // path aliases for the workspace
  "compilerOptions": {
    "baseUrl": ".", /* repo root */
    "paths": {"@lib/*": ["src/lib/*"]}
  }
}
`,
		"src/lib/prompts.ts": "export const RUNBOOK = `" + guide + "`;\n",
		"src/app.ts": `import { anthropic } from "@ai-sdk/anthropic";
import { generateText } from "ai";
import { RUNBOOK } from "@lib/prompts";

export async function answerTicket(q: string) {
  return generateText({
    model: anthropic("claude-sonnet-5"),
    system: RUNBOOK,
    maxTokens: 700,
    prompt: q,
  });
}
`,
	})
	site := siteInFile(t, r, "src/app.ts")
	if site.Shape.SystemPromptChars != len(guide) {
		t.Errorf("system prompt = %d chars, want %d via the commented tsconfig",
			site.Shape.SystemPromptChars, len(guide))
	}
}

// The regex fallback (config style files with no call extent) must know
// max_completion_tokens.
func TestMaxCompletionTokensFallback(t *testing.T) {
	r := analyzeTemp(t, map[string]string{"llm.yaml": `llm:
  model: gpt-5-mini
  max_completion_tokens: 700
`})
	site := siteInFile(t, r, "llm.yaml")
	if site.Shape.MaxTokens == nil || *site.Shape.MaxTokens != 700 {
		t.Errorf("max tokens = %v, want 700 from max_completion_tokens", site.Shape.MaxTokens)
	}
}

// Prompt size is measured in runes: a non ASCII prompt must not count
// three bytes per letter against token thresholds.
func TestPromptCharsAreRunes(t *testing.T) {
	prompt := strings.Repeat("R\u00e9sumez les d\u00e9cisions cl\u00e9s en fran\u00e7ais. ", 3)
	r := analyzeTemp(t, map[string]string{"recap.py": `def recap_reunion(text):
    return client.messages.create(
        model="claude-haiku-4-5",
        max_tokens=300,
        system="` + prompt + `",
        messages=[{"role": "user", "content": text}],
    )
`})
	s := soleSite(t, r).Shape
	want := utf8.RuneCountInString(prompt)
	if want == len(prompt) {
		t.Fatal("test prompt must be non ASCII")
	}
	if s.SystemPromptChars != want {
		t.Errorf("system prompt = %d chars, want %d runes (byte length %d)",
			s.SystemPromptChars, want, len(prompt))
	}
}
