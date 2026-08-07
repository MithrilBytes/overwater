package scan

import (
	"strings"
	"testing"
)

func TestPragmaIgnoreAndVolume(t *testing.T) {
	r := analyzeTemp(t, map[string]string{"app.js": `const client = new (require("openai"))();

async function classifyThing(text) {
  // overwater:ignore
  return client.chat.completions.create({
    model: "gpt-5.1",
    max_tokens: 60,
    messages: [{ role: "user", content: text }],
  });
}

async function summarizeThing(text) {
  // overwater:volume=250000
  return client.chat.completions.create({
    model: "gpt-5-mini",
    max_tokens: 200,
    messages: [{ role: "user", content: text }],
  });
}
`})
	if len(r.Sites) != 2 {
		t.Fatalf("got %d sites, want 2", len(r.Sites))
	}
	if !r.Sites[0].Ignored {
		t.Error("first site should carry the ignore pragma")
	}
	if r.Sites[1].VolumeOverride != 250000 {
		t.Errorf("volume override = %d, want 250000", r.Sites[1].VolumeOverride)
	}
}

func TestNewArchetypeSignals(t *testing.T) {
	cases := []struct {
		name, file, content, want string
	}{
		{"moderation endpoint", "mod.js", `async function gateComment(text) {
  return client.moderations.create({ model: "gpt-5-nano", input: text });
}
`, ArchetypeModeration},
		{"transcription endpoint", "audio.py", `def process_upload(path):
    with open(path, "rb") as audio:
        return client.audio.transcriptions.create(model="gpt-4o", file=audio)
`, ArchetypeTranscription},
		{"rerank call", "search.ts", `export async function orderResults(query: string, docs: string[]) {
  return co.rerank({ model: "command-r-08-2024", query, documents: docs, topN: 5 });
}
`, ArchetypeReranking},
		{"translation name", "i18n.py", `def translate_description(text, lang):
    return client.messages.create(
        model="claude-haiku-4-5",
        max_tokens=400,
        messages=[{"role": "user", "content": "Translate to " + lang + ": " + text}],
    )
`, ArchetypeTranslation},
		{"vision ocr", "scan.py", `def ocr_shipping_label(image_b64):
    return client.messages.create(
        model="claude-sonnet-5",
        max_tokens=300,
        messages=[{"role": "user", "content": [{"type": "image", "source": image_b64}]}],
    )
`, ArchetypeVision},
		{"codegen name", "gen.ts", `export async function generateCodeFix(diff: string) {
  return client.chat.completions.create({
    model: "gpt-5-mini",
    max_completion_tokens: 900,
    messages: [{ role: "user", content: diff }],
  });
}
`, ArchetypeCodegen},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			r := analyzeTemp(t, map[string]string{tt.file: tt.content})
			site := soleSite(t, r)
			if site.Archetype != tt.want {
				t.Errorf("archetype = %s (%s confidence), want %s", site.Archetype, site.ArchetypeConfidence, tt.want)
			}
		})
	}
}

func TestConfigModelTracesToReader(t *testing.T) {
	r := analyzeTemp(t, map[string]string{
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
	var reader *Site
	for i := range r.Sites {
		if r.Sites[i].File == "worker.js" {
			reader = &r.Sites[i]
		}
	}
	if reader == nil {
		t.Fatalf("no traced reader site: %+v", r.Sites)
	}
	if !reader.Known || reader.ModelID != "gpt-5.1" {
		t.Errorf("reader site = %+v, want gpt-5.1 resolved from .env", reader)
	}
	if reader.Shape.MaxTokens == nil || *reader.Shape.MaxTokens != 400 {
		t.Errorf("reader shape max tokens = %v, want 400", reader.Shape.MaxTokens)
	}
	if !strings.Contains(reader.ViaConfig, "SUMMARY_MODEL") {
		t.Errorf("ViaConfig = %q", reader.ViaConfig)
	}
	if reader.Archetype != ArchetypeSummarization {
		t.Errorf("archetype = %s, want summarization", reader.Archetype)
	}
}

func TestAzureDeploymentTraces(t *testing.T) {
	r := analyzeTemp(t, map[string]string{
		".env": "AZURE_OPENAI_DEPLOYMENT=acme-gpt4-prod\n",
		"azure.py": `import os
from openai import AzureOpenAI

client = AzureOpenAI()


def draft_response(prompt):
    return client.chat.completions.create(
        model=os.environ["AZURE_OPENAI_DEPLOYMENT"],
        max_tokens=500,
        messages=[{"role": "user", "content": prompt}],
    )
`,
	})
	var reader *Site
	for i := range r.Sites {
		if r.Sites[i].File == "azure.py" {
			reader = &r.Sites[i]
		}
	}
	if reader == nil {
		t.Fatalf("no traced deployment site: %+v", r.Sites)
	}
	if reader.Known || reader.Ref != "acme-gpt4-prod" {
		t.Errorf("reader = %+v, want the unknown deployment name surfaced", reader)
	}
}

func TestNearbyStrings(t *testing.T) {
	r := analyzeTemp(t, map[string]string{"app.py": `import anthropic

client = anthropic.Anthropic()


def summarize_report(text):
    return client.messages.create(
        model="claude-haiku-4-5",
        max_tokens=300,
        system="Condense the quarterly report into five sentences.",
        messages=[{"role": "user", "content": text}],
    )
`})
	site := soleSite(t, r)
	found := false
	for _, s := range site.NearbyStrings {
		if strings.Contains(s, "quarterly report") {
			found = true
		}
	}
	if !found {
		t.Errorf("nearby strings = %v, want the system prompt captured", site.NearbyStrings)
	}
}
