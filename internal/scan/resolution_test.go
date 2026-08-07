package scan

import (
	"reflect"
	"strings"
	"testing"
)

// A JSON schema example inside prompt prose is prose, not a schema: the
// fallback schema facts must read the prose masked region, where long
// string interiors are blank.
func TestSchemaInPromptDoesNotCount(t *testing.T) {
	r := analyzeTemp(t, map[string]string{"help.py": `def answer_question(q):
    return client.messages.create(
        model="claude-sonnet-5",
        max_tokens=400,
        system="""You help users write JSON schemas. Example:
{"properties": {"status": {"type": "string", "enum": ["open", "closed"]}}}
Keep every answer short.""",
        messages=[{"role": "user", "content": q}],
    )
`})
	site := soleSite(t, r)
	if site.Shape.SchemaEnumOnly || site.Shape.SchemaMultiField {
		t.Errorf("shape = %+v, want no schema facts from prose", site.Shape)
	}
	if site.Archetype == ArchetypeClassification {
		t.Error("archetype = classification, faked by a schema example inside the prompt")
	}
}

// A real inline schema, written as code, still yields schema facts
// through the prose masked fallback.
func TestInlineSchemaStillCounts(t *testing.T) {
	r := analyzeTemp(t, map[string]string{"fields.py": `def pull_fields(text):
    return client.messages.create(
        model="claude-sonnet-5",
        max_tokens=500,
        tools=[{
            "name": "record",
            "input_schema": {
                "type": "object",
                "properties": {
                    "vendor": {"type": "string"},
                    "total": {"type": "integer"},
                },
            },
        }],
        messages=[{"role": "user", "content": text}],
    )
`})
	if s := soleSite(t, r).Shape; !s.SchemaMultiField {
		t.Errorf("shape = %+v, want SchemaMultiField from the inline schema", s)
	}
}

func siteInFile(t *testing.T, r *Report, file string) *Site {
	t.Helper()
	for i := range r.Sites {
		if r.Sites[i].File == file {
			return &r.Sites[i]
		}
	}
	t.Fatalf("no site in %s: %+v", file, r.Sites)
	return nil
}

// An incremental scan restricted to one file must resolve that file
// with the same power a full scan has: the imported prompt lives in a
// file outside the only set.
func TestIncrementalKeepsImportResolution(t *testing.T) {
	guide := strings.Repeat("Summarize the support thread into three bullets for the on call engineer. ", 3)
	files := map[string]string{
		"prompts.ts": "export const GUIDE = `" + guide + "`;\n",
		"app.ts": `import { anthropic } from "@ai-sdk/anthropic";
import { generateText } from "ai";
import { GUIDE } from "./prompts";

export async function digestThread(text: string) {
  return generateText({
    model: anthropic("claude-sonnet-5"),
    system: GUIDE,
    maxTokens: 700,
    prompt: text,
  });
}
`,
	}
	dir := writeTree(t, files)
	cat := mustCatalog(t)
	full, err := Analyze(dir, cat)
	if err != nil {
		t.Fatal(err)
	}
	inc, err := AnalyzeOnly(dir, cat, map[string]bool{"app.ts": true})
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range inc.Sites {
		if s.File != "app.ts" {
			t.Errorf("incremental scan produced a site outside the only set: %s:%d", s.File, s.Line)
		}
	}
	fullSite := siteInFile(t, full, "app.ts")
	incSite := siteInFile(t, inc, "app.ts")
	if !reflect.DeepEqual(fullSite.Shape, incSite.Shape) {
		t.Errorf("shapes diverge:\nfull: %+v\n inc: %+v", fullSite.Shape, incSite.Shape)
	}
	if fullSite.Archetype != incSite.Archetype {
		t.Errorf("archetype full = %s, incremental = %s", fullSite.Archetype, incSite.Archetype)
	}
	if incSite.Shape.SystemPromptChars != len(guide) {
		t.Errorf("incremental system prompt = %d chars, want %d via the import hop",
			incSite.Shape.SystemPromptChars, len(guide))
	}
}

// Config tracing must also survive incremental scans: the .env file is
// outside the only set, yet the reader site still resolves.
func TestIncrementalKeepsConfigTracing(t *testing.T) {
	dir := writeTree(t, map[string]string{
		".env": "SUMMARY_MODEL=gpt-5.1\n",
		"worker.js": `const client = new (require("openai"))();

async function summarizeQueueItem(text) {
  return client.chat.completions.create({
    model: process.env.SUMMARY_MODEL,
    max_tokens: 400,
    messages: [{ role: "user", content: text }],
  });
}
`,
	})
	inc, err := AnalyzeOnly(dir, mustCatalog(t), map[string]bool{"worker.js": true})
	if err != nil {
		t.Fatal(err)
	}
	reader := siteInFile(t, inc, "worker.js")
	if !reader.Known || reader.ModelID != "gpt-5.1" || reader.ViaConfig == "" {
		t.Errorf("reader = %+v, want gpt-5.1 traced from the unchanged .env", reader)
	}
	for _, s := range inc.Sites {
		if s.File != "worker.js" {
			t.Errorf("incremental scan produced a site outside the only set: %s:%d", s.File, s.Line)
		}
	}
}
