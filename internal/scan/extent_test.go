package scan

import (
	"strings"
	"testing"
)

// The model string sits inside a nested provider call; the extent must
// ascend to the params object that names the model key.
func TestCallExtentFindsModelKey(t *testing.T) {
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
func TestCallExtentIgnoresStringBraces(t *testing.T) {
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

// The leftward walk stops at extentMaxBytes. An opener that far back
// could never produce an extent anyway, since matchClose gives up at
// the same distance, and walking on to byte zero costs a full prefix
// scan per ascent level per reference.
func TestEnclosingOpenStopsAtTheBound(t *testing.T) {
	near := "(" + strings.Repeat(" ", extentMaxBytes-10) + "x"
	if _, ok := enclosingOpen(near, len(near)); !ok {
		t.Error("an opener inside the bound was not found")
	}
	far := "(" + strings.Repeat(" ", extentMaxBytes+10) + "x"
	if _, ok := enclosingOpen(far, len(far)); ok {
		t.Error("an opener past the bound was walked back to; the scan is quadratic in references per file again")
	}
}
