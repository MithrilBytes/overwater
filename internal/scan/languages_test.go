package scan

import (
	"strings"
	"testing"
)

// The language matrix: each supported syntax family yields a parsed
// shape, not a regex guess.

func soleSite(t *testing.T, r *Report) Site {
	t.Helper()
	if len(r.Sites) != 1 {
		t.Fatalf("got %d sites, want 1: %+v", len(r.Sites), r.Sites)
	}
	return r.Sites[0]
}

func TestGoStructParams(t *testing.T) {
	r := analyzeTemp(t, map[string]string{"call.go": `package llm

func summarizeThread(ctx context.Context, text string) (string, error) {
	resp, err := client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:       "claude-sonnet-5",
		MaxTokens:   anthropic.Int(1024),
		Temperature: anthropic.Float(0.2),
	})
	return resp, err
}
`})
	s := soleSite(t, r).Shape
	if s.MaxTokens == nil || *s.MaxTokens != 1024 {
		t.Errorf("max tokens = %v, want 1024 through the wrapper", s.MaxTokens)
	}
	if s.Temperature == nil || *s.Temperature != 0.2 {
		t.Errorf("temperature = %v, want 0.2 through the wrapper", s.Temperature)
	}
}

func TestCSharpObjectInitializer(t *testing.T) {
	r := analyzeTemp(t, map[string]string{"Call.cs": `public async Task<string> TriageTicket(string body)
{
    var request = new MessageParams
    {
        Model = "claude-haiku-4-5",
        MaxTokens = 512,
        Temperature = 0,
    };
    return await client.Messages.CreateAsync(request);
}
`})
	site := soleSite(t, r)
	if site.Shape.MaxTokens == nil || *site.Shape.MaxTokens != 512 {
		t.Errorf("max tokens = %v, want 512", site.Shape.MaxTokens)
	}
	if site.Shape.Temperature == nil || *site.Shape.Temperature != 0 {
		t.Errorf("temperature = %v, want 0", site.Shape.Temperature)
	}
	if site.Archetype != ArchetypeClassification {
		t.Errorf("archetype = %s, want classification from TriageTicket", site.Archetype)
	}
}

func TestRubyKeywordArgs(t *testing.T) {
	r := analyzeTemp(t, map[string]string{"call.rb": `def summarize_issue(text)
  message = client.messages.create(
    model: "claude-sonnet-5",
    max_tokens: 800,
    system: "Condense the issue thread into three sentences.",
    messages: [{role: "user", content: text}]
  )
  message.content.first.text
end
`})
	site := soleSite(t, r)
	if site.Shape.MaxTokens == nil || *site.Shape.MaxTokens != 800 {
		t.Errorf("max tokens = %v, want 800", site.Shape.MaxTokens)
	}
	if site.Archetype != ArchetypeSummarization {
		t.Errorf("archetype = %s, want summarization", site.Archetype)
	}
}

func TestJavaBuilderChain(t *testing.T) {
	r := analyzeTemp(t, map[string]string{"Call.java": `class Digest {
    String summarizeInbox(String text) {
        MessageCreateParams params = MessageCreateParams.builder()
            .model("claude-sonnet-5")
            .maxTokens(1024L)
            .temperature(0.3)
            .build();
        return client.messages().create(params).text();
    }
}
`})
	site := soleSite(t, r)
	if site.Shape.MaxTokens == nil || *site.Shape.MaxTokens != 1024 {
		t.Errorf("max tokens = %v, want 1024 from the builder", site.Shape.MaxTokens)
	}
	if site.Shape.Temperature == nil || *site.Shape.Temperature != 0.3 {
		t.Errorf("temperature = %v, want 0.3 from the builder", site.Shape.Temperature)
	}
}

func TestRustBuilderChain(t *testing.T) {
	r := analyzeTemp(t, map[string]string{"call.rs": `fn classify_alert(text: &str) -> Result<String> {
    let request = CreateMessage::builder()
        .model("claude-haiku-4-5")
        .max_tokens(256)
        .build()?;
    client.send(request)
}
`})
	site := soleSite(t, r)
	if site.Shape.MaxTokens == nil || *site.Shape.MaxTokens != 256 {
		t.Errorf("max tokens = %v, want 256 from the builder", site.Shape.MaxTokens)
	}
	if site.Archetype != ArchetypeClassification {
		t.Errorf("archetype = %s, want classification", site.Archetype)
	}
}

func TestShellCurlPayload(t *testing.T) {
	r := analyzeTemp(t, map[string]string{"digest.sh": `#!/bin/sh
# nightly digest
curl -s https://api.openai.com/v1/chat/completions \
  -H "Authorization: Bearer $OPENAI_API_KEY" \
  -d '{"model": "gpt-5-mini", "max_tokens": 120, "messages": [{"role": "user", "content": "hi"}]}'
`})
	site := soleSite(t, r)
	if site.ModelID != "gpt-5-mini" {
		t.Fatalf("model = %s", site.ModelID)
	}
	if site.Shape.MaxTokens == nil || *site.Shape.MaxTokens != 120 {
		t.Errorf("max tokens = %v, want 120 from the curl payload", site.Shape.MaxTokens)
	}
}

func TestNotebookCells(t *testing.T) {
	nb := `{"cells": [
  {"cell_type": "markdown", "source": ["# analysis\n"]},
  {"cell_type": "code", "source": ["import anthropic\n", "client = anthropic.Anthropic()\n"]},
  {"cell_type": "code", "source": [
    "def summarize_batch(texts):\n",
    "    return client.messages.create(\n",
    "        model=\"claude-haiku-4-5\",\n",
    "        max_tokens=300,\n",
    "        messages=[{\"role\": \"user\", \"content\": texts}],\n",
    "    )\n"
  ]}
]}`
	r := analyzeTemp(t, map[string]string{"analysis.ipynb": nb})
	site := soleSite(t, r)
	if site.Shape.MaxTokens == nil || *site.Shape.MaxTokens != 300 {
		t.Errorf("max tokens = %v, want 300 from the notebook cell", site.Shape.MaxTokens)
	}
	if site.Archetype != ArchetypeSummarization {
		t.Errorf("archetype = %s, want summarization", site.Archetype)
	}
}

func TestWrapperCallInnermostExtent(t *testing.T) {
	r := analyzeTemp(t, map[string]string{"app.js": `const { askModel } = require("./llm");

async function draftNote(text) {
  return askModel("gpt-5-mini", text);
}
`})
	site := soleSite(t, r)
	if site.Hash == "" {
		t.Error("wrapper call should still get a content hash from its extent")
	}
}

func TestTsconfigAliasPrompt(t *testing.T) {
	guide := strings.Repeat("Answer with the runbook steps before improvising. ", 6)
	r := analyzeTemp(t, map[string]string{
		"tsconfig.json":      `{"compilerOptions": {"baseUrl": ".", "paths": {"@lib/*": ["src/lib/*"]}}}`,
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
	var site *Site
	for i := range r.Sites {
		if r.Sites[i].File == "src/app.ts" {
			site = &r.Sites[i]
		}
	}
	if site == nil {
		t.Fatal("no site in src/app.ts")
	}
	if site.Shape.SystemPromptChars != len(guide) {
		t.Errorf("system prompt = %d chars, want %d via the tsconfig alias", site.Shape.SystemPromptChars, len(guide))
	}
}

func TestNewManifestEcosystems(t *testing.T) {
	cases := []struct {
		file, content, ecosystem string
	}{
		{"pom.xml", `<dependency><groupId>com.anthropic</groupId></dependency>`, "maven"},
		{"build.gradle.kts", `implementation("dev.langchain4j:langchain4j:1.0")`, "maven"},
		{"App.csproj", `<PackageReference Include="Anthropic.SDK" Version="5.0" />`, "nuget"},
		{"composer.json", `{"require": {"openai-php/client": "^0.10"}}`, "composer"},
		{"Cargo.toml", `[dependencies]` + "\n" + `async-openai = "0.24"`, "cargo"},
		{"Package.swift", `.package(url: "https://github.com/MacPaw/OpenAI", from: "0.3.0")`, "swiftpm"},
	}
	for _, tt := range cases {
		sdks := scanManifest(tt.file, []byte(tt.content))
		if len(sdks) == 0 || sdks[0].Ecosystem != tt.ecosystem {
			t.Errorf("%s: got %+v, want ecosystem %s", tt.file, sdks, tt.ecosystem)
		}
	}
}

func TestMultiHopImportPrompt(t *testing.T) {
	guide := strings.Repeat("Cite the source document for every claim you make. ", 6)
	r := analyzeTemp(t, map[string]string{
		"deep/base.ts": "export const CITE = `" + guide + "`;\n",
		"deep/mid.ts":  `export { CITE } from "./base";` + "\n",
		"app.ts": `import { anthropic } from "@ai-sdk/anthropic";
import { generateText } from "ai";
import { CITE } from "./deep/mid";

export async function answerQuery(q: string) {
  return generateText({
    model: anthropic("claude-sonnet-5"),
    system: CITE,
    maxTokens: 500,
    prompt: q,
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
		t.Fatal("no site in app.ts")
	}
	if site.Shape.SystemPromptChars != len(guide) {
		t.Errorf("system prompt = %d chars, want %d across two hops", site.Shape.SystemPromptChars, len(guide))
	}
}
