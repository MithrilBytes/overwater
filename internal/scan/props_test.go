package scan

import (
	"strings"
	"testing"
)

func parseTestCall(t *testing.T, content string) *callInfo {
	t.Helper()
	m := maskFile("a.ts", content)
	objStart := strings.IndexByte(content, '{')
	objEnd, ok := matchClose(m.all, objStart)
	if !ok {
		t.Fatal("unbalanced test content")
	}
	info := parseCall(content, m, objStart, objEnd+1)
	if info == nil {
		t.Fatal("parseCall returned nil")
	}
	return info
}

func TestParseCallCalleeAndProps(t *testing.T) {
	info := parseTestCall(t, `await client.chat.completions.create({
  model: "gpt-5.1",
  temperature: 0.4,
  "max_tokens": 128,
  messages: [{ role: "user", content: text }],
})`)
	if info.Callee != "client.chat.completions.create" {
		t.Errorf("callee = %q", info.Callee)
	}
	if info.Props["temperature"] != "0.4" {
		t.Errorf("temperature = %q", info.Props["temperature"])
	}
	if info.Props["max_tokens"] != "128" {
		t.Errorf("quoted key max_tokens = %q", info.Props["max_tokens"])
	}
	if !strings.HasPrefix(info.Props["messages"], "[") {
		t.Errorf("messages value = %q", info.Props["messages"])
	}
}

func TestParseCallCalleeAcrossLines(t *testing.T) {
	info := parseTestCall(t, `const r = client
  .messages
  .create({
  model: "claude-sonnet-5",
})`)
	if info.Callee != "client.messages.create" {
		t.Errorf("callee = %q", info.Callee)
	}
}

func TestParseCallIgnoresNested(t *testing.T) {
	info := parseTestCall(t, `create({
  model: "gpt-5.1",
  note: "brace { and colon : inside",
  nested: { temperature: 0.9 },
  after: 1,
})`)
	if _, ok := info.Props["temperature"]; ok {
		t.Error("nested temperature leaked to the top level")
	}
	if info.Props["after"] != "1" {
		t.Errorf("after = %q; string hazards broke the walk", info.Props["after"])
	}
}

func TestPropNumberInWrapper(t *testing.T) {
	content := `models.generateContent({
  model: "gemini-2.5-flash",
  generationConfig: { temperature: 0.2, maxOutputTokens: 256 },
})`
	info := parseTestCall(t, content)
	if v, ok := propNumber(info, "temperature"); !ok || v != 0.2 {
		t.Errorf("temperature = %v %v, want 0.2 via the wrapper", v, ok)
	}
	if v, ok := propNumber(info, "max_tokens", "maxTokens", "max_output_tokens", "maxOutputTokens"); !ok || v != 256 {
		t.Errorf("max tokens = %v %v, want 256 via the wrapper", v, ok)
	}
}

func TestGeminiStructuralParse(t *testing.T) {
	r := analyzeTemp(t, map[string]string{"intent.ts": `import { GoogleGenAI } from "@google/genai";

const ai = new GoogleGenAI({});

export async function classifyIntent(utterance: string) {
  return ai.models.generateContent({
    model: "gemini-2.5-flash",
    generationConfig: { temperature: 0, maxOutputTokens: 64 },
    contents: utterance,
  });
}
`})
	if len(r.Sites) != 1 {
		t.Fatalf("got %d sites, want 1", len(r.Sites))
	}
	s := r.Sites[0].Shape
	if s.Temperature == nil || *s.Temperature != 0 {
		t.Errorf("temperature = %v, want 0 from the nested config", s.Temperature)
	}
	if s.MaxTokens == nil || *s.MaxTokens != 64 {
		t.Errorf("max tokens = %v, want 64 from the nested config", s.MaxTokens)
	}
}
