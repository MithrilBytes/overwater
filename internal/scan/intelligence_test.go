package scan

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func analyzeTemp(t *testing.T, files map[string]string) *Report {
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
	r, err := Analyze(dir, mustCatalog(t))
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// Two calls close together each keep their own parameters: a fixed line
// window would let the first call's max_tokens bleed into the second.
func TestAdjacentCallsDoNotBleed(t *testing.T) {
	r := analyzeTemp(t, map[string]string{"app.js": `const OpenAI = require("openai");
const client = new OpenAI();

async function tagOne(text) {
  return client.chat.completions.create({
    model: "gpt-5.1",
    temperature: 0.9,
    max_tokens: 50,
    messages: [{ role: "user", content: text }],
  });
}

async function tagTwo(text) {
  return client.chat.completions.create({
    model: "gpt-4o",
    temperature: 0.1,
    messages: [{ role: "user", content: text }],
  });
}
`})
	if len(r.Sites) != 2 {
		t.Fatalf("got %d sites, want 2: %+v", len(r.Sites), r.Sites)
	}
	one, two := r.Sites[0], r.Sites[1]
	if one.ModelID != "gpt-5.1" || two.ModelID != "gpt-4o" {
		t.Fatalf("models = %s, %s", one.ModelID, two.ModelID)
	}
	if one.Shape.Temperature == nil || *one.Shape.Temperature != 0.9 {
		t.Errorf("first temperature = %v, want 0.9", one.Shape.Temperature)
	}
	if one.Shape.MaxTokens == nil || *one.Shape.MaxTokens != 50 {
		t.Errorf("first max_tokens = %v, want 50", one.Shape.MaxTokens)
	}
	if two.Shape.Temperature == nil || *two.Shape.Temperature != 0.1 {
		t.Errorf("second temperature = %v, want 0.1", two.Shape.Temperature)
	}
	if two.Shape.MaxTokens != nil {
		t.Errorf("second max_tokens = %v, want absent; the neighbor's cap bled through", *two.Shape.MaxTokens)
	}
}

// Parameter words inside prompt prose are not parameters.
func TestPromptCannotFakeParams(t *testing.T) {
	r := analyzeTemp(t, map[string]string{"app.js": `const client = new (require("openai"))();

async function explain(question) {
  return client.chat.completions.create({
    model: "gpt-5.1",
    messages: [
      { role: "user", content: "Explain why temperature: 0.9 and max_tokens: 99 in our docs are illustrative sampling values only." },
    ],
  });
}
`})
	if len(r.Sites) != 1 {
		t.Fatalf("got %d sites, want 1", len(r.Sites))
	}
	s := r.Sites[0].Shape
	if s.Temperature != nil {
		t.Errorf("temperature = %v, want absent; prose leaked into the shape", *s.Temperature)
	}
	if s.MaxTokens != nil {
		t.Errorf("max_tokens = %v, want absent; prose leaked into the shape", *s.MaxTokens)
	}
}

// A pragma pins the archetype when the heuristics would get it wrong.
func TestPragmaPinsArchetype(t *testing.T) {
	r := analyzeTemp(t, map[string]string{"app.js": `const client = new (require("openai"))();

async function classifyItems(items) {
  // overwater:archetype=summarization
  return client.chat.completions.create({
    model: "gpt-5.1",
    max_tokens: 400,
    messages: [{ role: "user", content: items.join("\n") }],
  });
}
`})
	if len(r.Sites) != 1 {
		t.Fatalf("got %d sites, want 1", len(r.Sites))
	}
	site := r.Sites[0]
	if site.Archetype != ArchetypeSummarization || site.ArchetypeConfidence != "high" {
		t.Errorf("archetype = %s %s, want summarization pinned high", site.Archetype, site.ArchetypeConfidence)
	}
}

// A system prompt imported from a sibling file is resolved one hop away
// and measured.
func TestImportedPromptResolves(t *testing.T) {
	guide := strings.Repeat("All support answers cite the handbook before speculating. ", 8)
	r := analyzeTemp(t, map[string]string{
		"prompts.ts": "export const GUIDE = `" + guide + "`;\n",
		"app.ts": `import { anthropic } from "@ai-sdk/anthropic";
import { generateText } from "ai";
import { GUIDE } from "./prompts";

export async function answer(question: string) {
  return generateText({
    model: anthropic("claude-sonnet-5"),
    system: GUIDE,
    maxTokens: 800,
    prompt: question,
  });
}
`,
	})
	var site *Site
	for i := range r.Sites {
		if r.Sites[i].File == "app.ts" {
			site = &r.Sites[i]
		}
	}
	if site == nil {
		t.Fatalf("no site found in app.ts: %+v", r.Sites)
	}
	if site.Shape.SystemPromptChars != len(guide) {
		t.Errorf("system prompt = %d chars, want %d from the imported constant", site.Shape.SystemPromptChars, len(guide))
	}
}

// An enum only output schema is classification shaped even when no
// keyword says so.
func TestEnumSchemaIsClassification(t *testing.T) {
	r := analyzeTemp(t, map[string]string{"judge.ts": `import { anthropic } from "@ai-sdk/anthropic";
import { generateObject } from "ai";
import { z } from "zod";

const Result = z.object({
  level: z.enum(["low", "medium", "high"]),
});

export async function judgeThing(input: string) {
  return generateObject({
    model: anthropic("claude-opus-5"),
    schema: Result,
    maxTokens: 100,
    prompt: input,
  });
}
`})
	if len(r.Sites) != 1 {
		t.Fatalf("got %d sites, want 1", len(r.Sites))
	}
	site := r.Sites[0]
	if !site.Shape.SchemaEnumOnly {
		t.Errorf("shape = %+v, want SchemaEnumOnly", site.Shape)
	}
	if site.Archetype != ArchetypeClassification {
		t.Errorf("archetype = %s, want classification from schema semantics alone", site.Archetype)
	}
}

// Schema semantics on the real fixtures. The clean-app draft site,
// whose only evidence is a shared system prompt, must grade low rather
// than assert an archetype.
func TestFixtureSchemaAndConfidence(t *testing.T) {
	firehose := analyzeFixture(t, "ts-chat-firehose")
	if !firehose.Sites[1].Shape.SchemaEnumOnly {
		t.Errorf("classify.ts shape = %+v, want SchemaEnumOnly", firehose.Sites[1].Shape)
	}
	if got := firehose.Sites[1].ArchetypeConfidence; got != "high" {
		t.Errorf("classify.ts archetype confidence = %s, want high", got)
	}
	// The count bounds the output estimate, so it has to survive the hop
	// through the named schema, not just the enum verdict.
	if got := firehose.Sites[1].Shape.SchemaFields; got != 2 {
		t.Errorf("classify.ts schema fields = %d, want the 2 in Ticket", got)
	}

	extraction := analyzeFixture(t, "py-extraction")
	if !extraction.Sites[0].Shape.SchemaMultiField {
		t.Errorf("extract.py shape = %+v, want SchemaMultiField", extraction.Sites[0].Shape)
	}
	if got := extraction.Sites[0].Shape.SchemaFields; got != 5 {
		t.Errorf("extract.py schema fields = %d, want the 5 in INVOICE_TOOL", got)
	}

	clean := analyzeFixture(t, "clean-app")
	draft := clean.Sites[1]
	if draft.ModelID != "claude-sonnet-5" {
		t.Fatalf("expected the draftReply site second, got %s", draft.ModelID)
	}
	if draft.ArchetypeConfidence != "low" {
		t.Errorf("draftReply archetype = %s %s, want low confidence", draft.Archetype, draft.ArchetypeConfidence)
	}
}
