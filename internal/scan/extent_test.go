package scan

import (
	"strings"
	"testing"
)

// The model string sits inside a nested provider call; the extent must
// ascend to the params object that names the model key.
func TestCallExtentAscendsToModelParameter(t *testing.T) {
	content := `const result = streamText({
  model: anthropic("claude-opus-5"),
  system: "short",
  maxTokens: 100,
});
`
	m := maskFile("a.ts", content)
	hit := strings.Index(content, "claude-opus-5")
	start, end, ok := callExtent(m.all, m.prose, hit)
	if !ok {
		t.Fatal("no extent found")
	}
	ext := content[start:end]
	if !strings.Contains(ext, "maxTokens") || !strings.Contains(ext, "system") {
		t.Errorf("extent = %q, want the whole params object", ext)
	}
}

// Braces inside string literals must not derail bracket counting.
func TestCallExtentIgnoresBracesInStrings(t *testing.T) {
	content := `await create({
  model: "gpt-5.1",
  note: "this { is not a bracket",
  max_tokens: 32,
});
`
	m := maskFile("a.js", content)
	hit := strings.Index(content, "gpt-5.1")
	start, end, ok := callExtent(m.all, m.prose, hit)
	if !ok {
		t.Fatal("no extent found")
	}
	if ext := content[start:end]; !strings.Contains(ext, "max_tokens") {
		t.Errorf("extent = %q, want it to reach max_tokens past the stray brace", ext)
	}
}

func TestEnclosingFuncName(t *testing.T) {
	content := `export async function summarizeAll(items) {
  const out = await create({ model: "gpt-5.1" });
}
`
	m := maskFile("a.js", content)
	hit := strings.Index(content, "gpt-5.1")
	if name := enclosingFuncName(m.prose, hit); name != "summarizeAll" {
		t.Errorf("enclosing function = %q, want summarizeAll", name)
	}
}
