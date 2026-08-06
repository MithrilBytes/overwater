package rules

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/MithrilBytes/overwater/catalog"
	"github.com/MithrilBytes/overwater/internal/scan"
)

func evaluateFixture(t *testing.T, name string) []Finding {
	t.Helper()
	cat, err := catalog.Embedded()
	if err != nil {
		t.Fatal(err)
	}
	report, err := scan.Analyze(filepath.Join("..", "fixtures", name), cat)
	if err != nil {
		t.Fatal(err)
	}
	engine, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	return engine.Evaluate(report, cat)
}

func TestLoadRulesAndEstimates(t *testing.T) {
	e, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(e.Rules) != 6 {
		t.Errorf("loaded %d rules, want the 6 shipped with v1", len(e.Rules))
	}
	if e.Est.Volume.CallsPerMonth != 10000 {
		t.Errorf("calls_per_month = %d, want 10000", e.Est.Volume.CallsPerMonth)
	}
}

// The expected findings below are the same facts the goldens under
// goldens/ state in prose. If one side changes, the other must too.

func TestEvaluateTsChatFirehose(t *testing.T) {
	got := evaluateFixture(t, "ts-chat-firehose")
	want := []Finding{
		{
			RuleID:        "unbounded-max-tokens",
			Confidence:    "medium",
			File:          "app/api/chat/route.ts",
			Line:          7,
			Archetype:     "chat",
			Evidence:      "streaming, no max_tokens",
			Model:         "claude-opus-5",
			MonthlyUSD:    126,
			CandidateText: "same model with a max_tokens cap; cost unchanged until a response runs long",
			Tripwire:      "None; a cap only fires when a response tries to exceed it",
			Flags:         []string{"No max_tokens set; worst case spend is unbounded"},
		},
		{
			RuleID:        "frontier-extraction",
			Confidence:    "high",
			File:          "src/classify.ts",
			Line:          57,
			Archetype:     "classification",
			Evidence:      "temp 0, JSON schema",
			Model:         "claude-opus-5",
			MonthlyUSD:    135,
			CandidateText: "claude-haiku-4-5, same capability tier for this task class, ~$27/mo",
			Tripwire:      "If eval agreement drops below 97%, stay put",
			Flags:         []string{"No prompt caching on a 1,191-token repeated system prompt"},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("findings mismatch\n got: %+v\nwant: %+v", got, want)
	}
}

func TestEvaluatePyExtraction(t *testing.T) {
	got := evaluateFixture(t, "py-extraction")
	want := []Finding{
		{
			RuleID:        "frontier-extraction",
			Confidence:    "high",
			File:          "extract.py",
			Line:          24,
			Archetype:     "extraction",
			Evidence:      "temp 0, JSON schema",
			Model:         "claude-opus-5",
			MonthlyUSD:    126,
			CandidateText: "claude-haiku-4-5, same capability tier for this task class, ~$25/mo",
			Tripwire:      "If eval agreement drops below 97%, stay put",
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("findings mismatch\n got: %+v\nwant: %+v", got, want)
	}
}

func TestEvaluateNodeCronSummarizer(t *testing.T) {
	got := evaluateFixture(t, "node-cron-summarizer")
	want := []Finding{
		{
			RuleID:        "deprecated-model",
			Confidence:    "high",
			File:          "legacy/summarize-v1.js",
			Line:          7,
			Archetype:     "summarization",
			Evidence:      "",
			Model:         "text-davinci-003",
			MonthlyUSD:    151,
			CandidateText: "gpt-5-mini, current replacement in the same tier, ~$6/mo",
			Tripwire:      "None; there is no configuration in which a retired model id keeps working",
			Flags:         []string{"Unavailable after 2024-01-04; a correctness bug, not just a cost one"},
		},
		{
			RuleID:        "batch-on-realtime",
			Confidence:    "medium",
			File:          "src/summarize.js",
			Line:          12,
			Archetype:     "summarization",
			Evidence:      "cron scheduled",
			Model:         "gpt-5.1",
			MonthlyUSD:    48,
			CandidateText: "gpt-5.1 through the batch endpoint at half price, ~$24/mo",
			Tripwire:      "If results are needed in under an hour, stay put",
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("findings mismatch\n got: %+v\nwant: %+v", got, want)
	}
}

func TestEvaluateRagFrontierEmbeddings(t *testing.T) {
	got := evaluateFixture(t, "rag-frontier-embeddings")
	want := []Finding{
		{
			RuleID:        "pricey-embeddings",
			Confidence:    "high",
			File:          "ingest.py",
			Line:          8,
			Archetype:     "embedding",
			Evidence:      "embeddings API",
			Model:         "text-embedding-3-large",
			MonthlyUSD:    13,
			CandidateText: "text-embedding-3-small, same provider at the standard embedding tier, ~$2/mo",
			Tripwire:      "If retrieval quality drops on your eval set, stay put",
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("findings mismatch\n got: %+v\nwant: %+v", got, want)
	}
}

func TestEvaluateCleanAppKeepsItsModels(t *testing.T) {
	if got := evaluateFixture(t, "clean-app"); len(got) != 0 {
		t.Errorf("clean-app produced findings, want the null verdict: %+v", got)
	}
}

func TestFlagWithNoHostFindingBecomesAFinding(t *testing.T) {
	engine, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	cat, err := catalog.Embedded()
	if err != nil {
		t.Fatal(err)
	}
	// A mid tier anthropic model with a huge uncached prompt trips only
	// the caching flag; it must still surface as a finding.
	report := &scan.Report{Sites: []scan.Site{{
		File: "app.ts", Line: 3, Ref: "claude-sonnet-5", ModelID: "claude-sonnet-5",
		Known: true, Archetype: scan.ArchetypeChat,
		Shape: scan.Shape{
			Readable:          true,
			MaxTokens:         intPtr(1000),
			SystemPromptChars: 8000,
		},
	}}}
	got := engine.Evaluate(report, cat)
	if len(got) != 1 || got[0].RuleID != "uncached-system-prompt" {
		t.Fatalf("got %+v, want one uncached-system-prompt finding", got)
	}
	if got[0].CandidateText != "same model with cache_control on the system prompt" {
		t.Errorf("candidate = %q", got[0].CandidateText)
	}
	if len(got[0].Flags) != 1 || got[0].Flags[0] != "No prompt caching on a 2,000-token repeated system prompt" {
		t.Errorf("flags = %v", got[0].Flags)
	}
}

func intPtr(v int) *int { return &v }

// A rule that leans on the archetype inherits the classifier's doubt.
func TestLowConfidenceArchetypeDemotesFinding(t *testing.T) {
	engine, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	cat, err := catalog.Embedded()
	if err != nil {
		t.Fatal(err)
	}
	report := &scan.Report{Sites: []scan.Site{{
		File: "app.ts", Line: 9, Ref: "claude-opus-5", ModelID: "claude-opus-5",
		Known: true, Archetype: scan.ArchetypeClassification, ArchetypeConfidence: "low",
		Shape: scan.Shape{Readable: true, MaxTokens: intPtr(200)},
	}}}
	got := engine.Evaluate(report, cat)
	if len(got) != 1 || got[0].RuleID != "frontier-extraction" {
		t.Fatalf("got %+v, want one frontier-extraction finding", got)
	}
	if got[0].Confidence != "medium" {
		t.Errorf("confidence = %s, want medium after demotion from high", got[0].Confidence)
	}
}
