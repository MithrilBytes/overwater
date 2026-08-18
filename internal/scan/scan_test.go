package scan

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MithrilBytes/overwater/catalog"
)

func mustCatalog(t *testing.T) *catalog.Catalog {
	t.Helper()
	c, err := catalog.Embedded()
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func analyzeFixture(t *testing.T, name string) *Report {
	t.Helper()
	r, err := Analyze(filepath.Join("..", "..", "fixtures", name), mustCatalog(t))
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func hasSDK(r *Report, ecosystem, name string) bool {
	for _, s := range r.SDKs {
		if s.Ecosystem == ecosystem && s.Name == name {
			return true
		}
	}
	return false
}

func TestAnalyzeTsChatFirehose(t *testing.T) {
	r := analyzeFixture(t, "ts-chat-firehose")
	if !hasSDK(r, "npm", "ai") || !hasSDK(r, "npm", "@ai-sdk/anthropic") {
		t.Errorf("manifest layer missed the Vercel AI SDK: %+v", r.SDKs)
	}
	if len(r.Sites) != 2 {
		t.Fatalf("got %d sites, want 2: %+v", len(r.Sites), r.Sites)
	}

	chat := r.Sites[0]
	if chat.File != "app/api/chat/route.ts" || chat.Line != 7 || chat.ModelID != "claude-opus-5" {
		t.Errorf("chat site = %s:%d %s", chat.File, chat.Line, chat.ModelID)
	}
	if !chat.Shape.Streaming || chat.Shape.MaxTokens != nil || !chat.Shape.Readable {
		t.Errorf("chat shape = %+v, want streaming, no max_tokens, readable", chat.Shape)
	}
	if chat.Shape.SystemPromptChars != 54 {
		t.Errorf("chat system prompt = %d chars, want 54", chat.Shape.SystemPromptChars)
	}

	classify := r.Sites[1]
	if classify.File != "src/classify.ts" || classify.Line != 57 {
		t.Errorf("classify site = %s:%d", classify.File, classify.Line)
	}
	s := classify.Shape
	if s.Temperature == nil || *s.Temperature != 0 {
		t.Errorf("classify temperature = %v, want 0", s.Temperature)
	}
	if s.MaxTokens == nil || *s.MaxTokens != 200 {
		t.Errorf("classify max_tokens = %v, want 200", s.MaxTokens)
	}
	if !s.JSONSchema || s.CacheControl {
		t.Errorf("classify shape = %+v, want schema and no caching", s)
	}
	if s.SystemPromptChars != 4767 {
		t.Errorf("classify system prompt = %d chars, want 4767", s.SystemPromptChars)
	}
}

func TestAnalyzePyExtraction(t *testing.T) {
	r := analyzeFixture(t, "py-extraction")
	if !hasSDK(r, "pypi", "anthropic") {
		t.Errorf("manifest layer missed the anthropic SDK: %+v", r.SDKs)
	}
	if len(r.Sites) != 1 {
		t.Fatalf("got %d sites, want 1: %+v", len(r.Sites), r.Sites)
	}
	site := r.Sites[0]
	if site.File != "extract.py" || site.Line != 24 || site.ModelID != "claude-opus-5" {
		t.Errorf("site = %s:%d %s", site.File, site.Line, site.ModelID)
	}
	s := site.Shape
	if s.Temperature == nil || *s.Temperature != 0 {
		t.Errorf("temperature = %v, want 0", s.Temperature)
	}
	if s.MaxTokens == nil || *s.MaxTokens != 1024 {
		t.Errorf("max_tokens = %v, want 1024", s.MaxTokens)
	}
	if !s.JSONSchema || !s.ForcedTool || !s.Tools {
		t.Errorf("shape = %+v, want schema, tools, and a forced tool choice", s)
	}
	if s.SystemPromptChars != 63 {
		t.Errorf("system prompt = %d chars, want 63", s.SystemPromptChars)
	}
}

func TestAnalyzeNodeCronSummarizer(t *testing.T) {
	r := analyzeFixture(t, "node-cron-summarizer")
	if !hasSDK(r, "npm", "openai") {
		t.Errorf("manifest layer missed the openai SDK: %+v", r.SDKs)
	}
	if len(r.Sites) != 2 {
		t.Fatalf("got %d sites, want 2: %+v", len(r.Sites), r.Sites)
	}

	legacy := r.Sites[0]
	if legacy.File != "legacy/summarize-v1.js" || legacy.Line != 7 || legacy.ModelID != "text-davinci-003" {
		t.Errorf("legacy site = %s:%d %s", legacy.File, legacy.Line, legacy.ModelID)
	}
	if legacy.Shape.BatchContext {
		t.Error("legacy file has no cron context, but BatchContext is set")
	}
	if legacy.Shape.MaxTokens == nil || *legacy.Shape.MaxTokens != 256 {
		t.Errorf("legacy max_tokens = %v, want 256", legacy.Shape.MaxTokens)
	}

	digest := r.Sites[1]
	if digest.File != "src/summarize.js" || digest.Line != 12 || digest.ModelID != "gpt-5.1" {
		t.Errorf("digest site = %s:%d %s", digest.File, digest.Line, digest.ModelID)
	}
	if !digest.Shape.BatchContext || digest.Shape.BatchAPI {
		t.Errorf("digest shape = %+v, want cron context and no batch API", digest.Shape)
	}
	if digest.Shape.SystemPromptChars != 558 {
		t.Errorf("digest system prompt = %d chars, want 558", digest.Shape.SystemPromptChars)
	}
}

func TestAnalyzeRagFrontierEmbeddings(t *testing.T) {
	r := analyzeFixture(t, "rag-frontier-embeddings")
	if len(r.Sites) != 1 {
		t.Fatalf("got %d sites, want 1: %+v", len(r.Sites), r.Sites)
	}
	site := r.Sites[0]
	if site.File != "ingest.py" || site.Line != 8 || site.ModelID != "text-embedding-3-large" {
		t.Errorf("site = %s:%d %s", site.File, site.Line, site.ModelID)
	}
	if !site.Shape.EmbeddingCall {
		t.Errorf("shape = %+v, want an embedding call", site.Shape)
	}
}

func TestAnalyzeCleanApp(t *testing.T) {
	r := analyzeFixture(t, "clean-app")
	if len(r.Sites) != 2 {
		t.Fatalf("got %d sites, want 2: %+v", len(r.Sites), r.Sites)
	}
	triage, draft := r.Sites[0], r.Sites[1]
	if triage.ModelID != "claude-haiku-4-5" || draft.ModelID != "claude-sonnet-5" {
		t.Errorf("models = %s, %s", triage.ModelID, draft.ModelID)
	}
	for _, site := range r.Sites {
		if !site.Shape.CacheControl {
			t.Errorf("%s:%d should show cache_control", site.File, site.Line)
		}
		if site.Shape.MaxTokens == nil {
			t.Errorf("%s:%d should have max_tokens set", site.File, site.Line)
		}
	}
}

// A file can be almost entirely model references, and describing a site
// is milliseconds of shape, extent and classification work, so one
// generated file under the size cap used to cost a minute of a runner.
// The analysis stops at maxSitesPerFile and names the file it stopped
// in, so the file reads as under analyzed rather than cheap.
func TestSitesPerFileAreCapped(t *testing.T) {
	var src strings.Builder
	for i := 0; i < maxSitesPerFile+40; i++ {
		fmt.Fprintf(&src, "m%d = call(model=\"gpt-4o\")\n", i)
	}
	r := analyzeTemp(t, map[string]string{"registry.py": src.String()})
	if len(r.Sites) != maxSitesPerFile {
		t.Errorf("got %d sites from %d references, want the cap of %d",
			len(r.Sites), maxSitesPerFile+40, maxSitesPerFile)
	}
	if len(r.Truncated) != 1 || r.Truncated[0] != "registry.py" {
		t.Errorf("truncated = %v, want registry.py named", r.Truncated)
	}
}
