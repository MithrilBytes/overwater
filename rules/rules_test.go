package rules

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
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
	findings := engine.Evaluate(report, cat)
	// The tables below pin the human visible fields; the baseline tests
	// cover the fingerprint. Check the hash is there, then blank it so
	// the tables stay readable.
	for i := range findings {
		if findings[i].SiteHash == "" {
			t.Errorf("finding %s at %s:%d has no site hash", findings[i].RuleID, findings[i].File, findings[i].Line)
		}
		findings[i].SiteHash = ""
	}
	return findings
}

func TestLoadRulesAndEstimates(t *testing.T) {
	e, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(e.Rules) != 12 {
		t.Errorf("loaded %d rules, want the 12 shipped", len(e.Rules))
	}
	if e.Est.Volume.CallsPerMonth != 10000 {
		t.Errorf("calls_per_month = %d, want 10000", e.Est.Volume.CallsPerMonth)
	}
}

// These tables and the goldens under goldens/ state the same facts.
// Change one, change the other.

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
			Volume:        10000,
			VolumeSource:  "estimate",
			CandidateText: "same model with a max_tokens cap; cost unchanged until a response runs long",
			Tripwire:      "None; a cap only fires when a response tries to exceed it",
			Flags:         []string{"No max_tokens set; worst case spend is unbounded"},
		},
		{
			RuleID:         "frontier-extraction",
			Confidence:     "high",
			File:           "src/classify.ts",
			Line:           57,
			Archetype:      "classification",
			Evidence:       "temp 0, JSON schema",
			Model:          "claude-opus-5",
			MonthlyUSD:     91,
			Volume:         10000,
			VolumeSource:   "estimate",
			CandidateText:  "claude-haiku-4-5, same capability tier for this task class, ~$18/mo",
			CandidateModel: "claude-haiku-4-5",
			Tripwire:       "If eval agreement drops below 97%, stay put",
			Flags:          []string{"No prompt caching on a 1,191-token repeated system prompt"},
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
			RuleID:         "frontier-extraction",
			Confidence:     "high",
			File:           "extract.py",
			Line:           24,
			Archetype:      "extraction",
			Evidence:       "temp 0, JSON schema",
			Model:          "claude-opus-5",
			MonthlyUSD:     78,
			Volume:         10000,
			VolumeSource:   "estimate",
			CandidateText:  "claude-haiku-4-5, same capability tier for this task class, ~$16/mo",
			CandidateModel: "claude-haiku-4-5",
			Tripwire:       "If eval agreement drops below 97%, stay put",
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
			RuleID:         "deprecated-model",
			Confidence:     "high",
			File:           "legacy/summarize-v1.js",
			Line:           7,
			Archetype:      "summarization",
			Evidence:       "",
			Model:          "text-davinci-003",
			MonthlyUSD:     151,
			Volume:         10000,
			VolumeSource:   "estimate",
			CandidateText:  "gpt-5-mini, current replacement in the same tier, ~$6/mo",
			CandidateModel: "gpt-5-mini",
			Tripwire:       "None; there is no configuration in which a retired model id keeps working",
			Flags:          []string{"Unavailable after 2024-01-04; a correctness bug, not just a cost one"},
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
			Volume:        10000,
			VolumeSource:  "estimate",
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
			RuleID:         "pricey-embeddings",
			Confidence:     "high",
			File:           "ingest.py",
			Line:           8,
			Archetype:      "embedding",
			Evidence:       "embeddings API",
			Model:          "text-embedding-3-large",
			MonthlyUSD:     13,
			Volume:         10000,
			VolumeSource:   "estimate",
			CandidateText:  "text-embedding-3-small, same provider at the standard embedding tier, ~$2/mo",
			CandidateModel: "text-embedding-3-small",
			Tripwire:       "If retrieval quality drops on your eval set, stay put",
			Flags:          []string{"No dimensions parameter on a model that supports one; vectors ship at full width"},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("findings mismatch\n got: %+v\nwant: %+v", got, want)
	}
}

func TestEvaluateCleanApp(t *testing.T) {
	if got := evaluateFixture(t, "clean-app"); len(got) != 0 {
		t.Errorf("clean-app produced findings, want the null verdict: %+v", got)
	}
}

func TestFlagWithoutHost(t *testing.T) {
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
	// claude-sonnet-5 publishes cache rates, so the candidate carries the
	// steady state price: 500 input tokens at $2, 2,000 system tokens at
	// the $0.20 read rate, 400 output tokens at $10, at 10,000 calls.
	if got[0].CandidateText != "same model with cache_control on the system prompt, ~$54/mo" {
		t.Errorf("candidate = %q", got[0].CandidateText)
	}
	if len(got[0].Flags) != 1 || got[0].Flags[0] != "No prompt caching on a 2,000-token repeated system prompt" {
		t.Errorf("flags = %v", got[0].Flags)
	}
}

// A model without published cache rates keeps the unpriced wording.
func TestUnpricedWithoutCacheRates(t *testing.T) {
	engine, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	cat := &catalog.Catalog{Version: "2026-01-01", Models: []catalog.Model{{
		ID: "claude-legacy-1", Provider: "anthropic",
		InputPerMtok: 3, OutputPerMtok: 15, ContextWindow: 200000,
		Tier: "mid", Released: "2024-01-01",
	}}}
	report := &scan.Report{Sites: []scan.Site{
		site("claude-legacy-1", scan.ArchetypeChat, scan.Shape{SystemPromptChars: 8000}),
	}}
	got := engine.Evaluate(report, cat)
	if len(got) != 1 || got[0].RuleID != "uncached-system-prompt" {
		t.Fatalf("got %+v, want one uncached-system-prompt finding", got)
	}
	if got[0].CandidateText != "same model with cache_control on the system prompt" {
		t.Errorf("candidate = %q, want the unpriced wording", got[0].CandidateText)
	}
}

func intPtr(v int) *int { return &v }

func floatPtr(v float64) *float64 { return &v }

func loadEngine(t *testing.T) (*Engine, *catalog.Catalog) {
	t.Helper()
	engine, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	cat, err := catalog.Embedded()
	if err != nil {
		t.Fatal(err)
	}
	return engine, cat
}

// site builds one known call site with a bounded, readable shape so
// only the rule under test fires. Each gets its own hash so unrelated
// sites never group as duplicates.
var siteSeq int

func site(ref, archetype string, shape scan.Shape) scan.Site {
	if shape.MaxTokens == nil {
		shape.MaxTokens = intPtr(300)
	}
	shape.Readable = true
	siteSeq++
	return scan.Site{
		File: "app.ts", Line: 5, Ref: ref, ModelID: ref,
		Known: true, Archetype: archetype,
		Hash: fmt.Sprintf("hash%04d", siteSeq), Shape: shape,
	}
}

func TestEffortOverkillOnExtraction(t *testing.T) {
	engine, cat := loadEngine(t)
	report := &scan.Report{Sites: []scan.Site{
		site("claude-sonnet-5", scan.ArchetypeExtraction, scan.Shape{Effort: "high"}),
	}}
	got := engine.Evaluate(report, cat)
	if len(got) != 1 || got[0].RuleID != "effort-overkill" {
		t.Fatalf("got %+v, want one effort-overkill finding", got)
	}
	if got[0].Confidence != "medium" {
		t.Errorf("confidence = %s, want medium", got[0].Confidence)
	}
	if got[0].CandidateText != "same model at default effort; extraction and classification rarely need deliberate reasoning" {
		t.Errorf("candidate = %q", got[0].CandidateText)
	}
}

func TestEffortOverkillIgnores(t *testing.T) {
	engine, cat := loadEngine(t)
	report := &scan.Report{Sites: []scan.Site{
		site("claude-sonnet-5", scan.ArchetypeClassification, scan.Shape{Effort: "medium"}),
		site("claude-sonnet-5", scan.ArchetypeChat, scan.Shape{Effort: "xhigh"}),
	}}
	if got := engine.Evaluate(report, cat); len(got) != 0 {
		t.Errorf("got %+v, want no findings", got)
	}
}

func TestRetryAmplificationPromoted(t *testing.T) {
	engine, cat := loadEngine(t)
	report := &scan.Report{Sites: []scan.Site{
		site("claude-opus-5", scan.ArchetypeChat, scan.Shape{MaxRetries: intPtr(3)}),
	}}
	got := engine.Evaluate(report, cat)
	if len(got) != 1 || got[0].RuleID != "retry-amplification" {
		t.Fatalf("got %+v, want one promoted retry-amplification finding", got)
	}
	want := "max_retries 3 on a frontier model multiplies worst case spend"
	if len(got[0].Flags) != 1 || got[0].Flags[0] != want {
		t.Errorf("flags = %v, want %q", got[0].Flags, want)
	}
}

func TestRetryAmplificationAttaches(t *testing.T) {
	engine, cat := loadEngine(t)
	report := &scan.Report{Sites: []scan.Site{
		site("claude-opus-5", scan.ArchetypeExtraction, scan.Shape{MaxRetries: intPtr(4)}),
	}}
	got := engine.Evaluate(report, cat)
	if len(got) != 1 || got[0].RuleID != "frontier-extraction" {
		t.Fatalf("got %+v, want the frontier-extraction finding to host the flag", got)
	}
	want := "max_retries 4 on a frontier model multiplies worst case spend"
	if len(got[0].Flags) != 1 || got[0].Flags[0] != want {
		t.Errorf("flags = %v, want %q", got[0].Flags, want)
	}
}

func TestRetryAmplificationIgnores(t *testing.T) {
	engine, cat := loadEngine(t)
	report := &scan.Report{Sites: []scan.Site{
		site("claude-opus-5", scan.ArchetypeChat, scan.Shape{MaxRetries: intPtr(2)}),
		site("claude-haiku-4-5", scan.ArchetypeChat, scan.Shape{MaxRetries: intPtr(9)}),
	}}
	if got := engine.Evaluate(report, cat); len(got) != 0 {
		t.Errorf("got %+v, want no findings below the threshold or tier", got)
	}
}

func TestHotTemperatureExtraction(t *testing.T) {
	engine, cat := loadEngine(t)
	report := &scan.Report{Sites: []scan.Site{
		site("claude-sonnet-5", scan.ArchetypeExtraction, scan.Shape{Temperature: floatPtr(0.8)}),
	}}
	got := engine.Evaluate(report, cat)
	if len(got) != 1 || got[0].RuleID != "hot-temperature-extraction" {
		t.Fatalf("got %+v, want one hot-temperature-extraction finding", got)
	}
	want := "Temperature above zero on extraction risks inconsistent output; a correctness issue before a cost one"
	if len(got[0].Flags) != 1 || got[0].Flags[0] != want {
		t.Errorf("flags = %v, want %q", got[0].Flags, want)
	}
}

func TestHotTemperatureIgnoresZero(t *testing.T) {
	engine, cat := loadEngine(t)
	report := &scan.Report{Sites: []scan.Site{
		site("claude-sonnet-5", scan.ArchetypeExtraction, scan.Shape{Temperature: floatPtr(0)}),
		site("claude-sonnet-5", scan.ArchetypeExtraction, scan.Shape{}),
	}}
	if got := engine.Evaluate(report, cat); len(got) != 0 {
		t.Errorf("got %+v, want no findings for temperature zero or unset", got)
	}
}

func TestImageDetailHigh(t *testing.T) {
	engine, cat := loadEngine(t)
	report := &scan.Report{Sites: []scan.Site{
		site("gpt-4o", scan.ArchetypeVision, scan.Shape{ImageDetailHigh: true}),
	}}
	got := engine.Evaluate(report, cat)
	if len(got) != 1 || got[0].RuleID != "image-detail-high" {
		t.Fatalf("got %+v, want one image-detail-high finding", got)
	}
	if got[0].Confidence != "low" {
		t.Errorf("confidence = %s, want low", got[0].Confidence)
	}
}

func TestUncappedEmbeddingDimensions(t *testing.T) {
	engine, cat := loadEngine(t)
	report := &scan.Report{Sites: []scan.Site{
		site("text-embedding-3-large", scan.ArchetypeEmbedding, scan.Shape{}),
	}}
	got := engine.Evaluate(report, cat)
	if len(got) != 1 || got[0].RuleID != "pricey-embeddings" {
		t.Fatalf("got %+v, want the pricey-embeddings finding to host the flag", got)
	}
	want := "No dimensions parameter on a model that supports one; vectors ship at full width"
	if len(got[0].Flags) != 1 || got[0].Flags[0] != want {
		t.Errorf("flags = %v, want %q", got[0].Flags, want)
	}
}

func TestCappedEmbeddingsStayQuiet(t *testing.T) {
	engine, cat := loadEngine(t)
	report := &scan.Report{Sites: []scan.Site{
		// Dimensions set: the flag stays away even though the model
		// supports the parameter.
		site("text-embedding-3-large", scan.ArchetypeEmbedding, scan.Shape{Dimensions: intPtr(512)}),
		// No dimensions capability at all: nothing to cap.
		site("mistral-embed", scan.ArchetypeEmbedding, scan.Shape{}),
	}}
	for _, f := range engine.Evaluate(report, cat) {
		if f.RuleID == "uncapped-embedding-dimensions" {
			t.Errorf("unexpected uncapped-embedding-dimensions at %s:%d", f.File, f.Line)
		}
		for _, flag := range f.Flags {
			if flag == "No dimensions parameter on a model that supports one; vectors ship at full width" {
				t.Errorf("unexpected dimensions flag on %s", f.RuleID)
			}
		}
	}
}

func TestImageDetailHighIgnoresChat(t *testing.T) {
	engine, cat := loadEngine(t)
	report := &scan.Report{Sites: []scan.Site{
		site("gpt-4o", scan.ArchetypeChat, scan.Shape{ImageDetailHigh: true}),
	}}
	if got := engine.Evaluate(report, cat); len(got) != 0 {
		t.Errorf("got %+v, want no findings on a chat archetype", got)
	}
}

func TestDisableRemovesRules(t *testing.T) {
	engine, cat := loadEngine(t)
	engine.Disable([]string{"deprecated-model", "not-a-rule"})
	report := &scan.Report{Sites: []scan.Site{
		site("text-davinci-003", scan.ArchetypeSummarization, scan.Shape{}),
	}}
	if got := engine.Evaluate(report, cat); len(got) != 0 {
		t.Errorf("got %+v, want no findings with deprecated-model disabled", got)
	}
}

func TestCloneIsolates(t *testing.T) {
	engine, cat := loadEngine(t)
	clone := engine.Clone()
	clone.Disable([]string{"deprecated-model"})
	if err := clone.SetThreshold("retry-amplification", "min_retries", 99); err != nil {
		t.Fatal(err)
	}
	clone.Est.Volume.CallsPerMonth = 1000000

	report := &scan.Report{Sites: []scan.Site{
		site("text-davinci-003", scan.ArchetypeSummarization, scan.Shape{}),
		site("claude-opus-5", scan.ArchetypeChat, scan.Shape{MaxRetries: intPtr(6)}),
	}}
	var ids []string
	for _, f := range engine.Evaluate(report, cat) {
		ids = append(ids, f.RuleID)
	}
	for _, want := range []string{"deprecated-model", "retry-amplification"} {
		if !slices.Contains(ids, want) {
			t.Errorf("origin findings = %v, want %s still firing after the clone changed", ids, want)
		}
	}
	if engine.Est.Volume.CallsPerMonth == 1000000 {
		t.Error("the clone's volume reached the engine it came from")
	}
}

func TestSetThreshold(t *testing.T) {
	engine, cat := loadEngine(t)
	if err := engine.SetThreshold("retry-amplification", "min_retries", 6); err != nil {
		t.Fatal(err)
	}
	report := &scan.Report{Sites: []scan.Site{
		site("claude-opus-5", scan.ArchetypeChat, scan.Shape{MaxRetries: intPtr(5)}),
	}}
	if got := engine.Evaluate(report, cat); len(got) != 0 {
		t.Errorf("got %+v, want no findings below the raised threshold", got)
	}
	report = &scan.Report{Sites: []scan.Site{
		site("claude-opus-5", scan.ArchetypeChat, scan.Shape{MaxRetries: intPtr(6)}),
	}}
	got := engine.Evaluate(report, cat)
	if len(got) != 1 || got[0].RuleID != "retry-amplification" {
		t.Errorf("got %+v, want the rule to fire at the raised threshold", got)
	}
}

func TestSetThresholdRejects(t *testing.T) {
	engine, _ := loadEngine(t)
	if err := engine.SetThreshold("retry-amplification", "min_carrots", 1); err == nil {
		t.Error("want an error for an unknown threshold field")
	}
	if err := engine.SetThreshold("no-such-rule", "min_retries", 1); err == nil {
		t.Error("want an error for an unknown rule id")
	}
}

// Every closed set is checked at load. Providers are not: the catalog
// owns that namespace.
func TestRuleValidateRejects(t *testing.T) {
	valid := func() Rule {
		return Rule{
			ID: "probe", Kind: "finding", Confidence: "low",
			Candidate: Candidate{Strategy: "none", Note: "n"},
			Tripwire:  "t",
		}
	}
	cases := []struct {
		name    string
		mutate  func(*Rule)
		wantErr string
	}{
		{"misspelled tier", func(r *Rule) { r.When.Tier = []string{"fronteir"} }, "unknown tier"},
		{"misspelled effort", func(r *Rule) { r.When.Effort = []string{"hgh"} }, "unknown effort"},
		{"misspelled capability", func(r *Rule) { r.When.ModelCapability = "dimenzions" }, "unknown model_capability"},
		{"misspelled archetype", func(r *Rule) { r.When.Archetype = []string{"clasification"} }, "unknown archetype"},
		{"misspelled archetype_not", func(r *Rule) { r.When.ArchetypeNot = []string{"embeding"} }, "unknown archetype"},
		{"multiplier missing", func(r *Rule) { r.Candidate.Strategy = "price_multiplier" }, "multiplier"},
		{"multiplier above one", func(r *Rule) {
			r.Candidate.Strategy = "price_multiplier"
			r.Candidate.Multiplier = 1.5
		}, "multiplier"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			r := valid()
			tt.mutate(&r)
			err := r.validate()
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("validate() = %v, want error containing %q", err, tt.wantErr)
			}
		})
	}
	// The exception: an unknown provider still loads.
	r := valid()
	r.When.Provider = []string{"anthorpic"}
	if err := r.validate(); err != nil {
		t.Errorf("validate() = %v, want provider names left to the catalog", err)
	}
}

func TestTotalMonthlyUSD(t *testing.T) {
	engine, cat := loadEngine(t)
	a := site("claude-sonnet-5", scan.ArchetypeChat, scan.Shape{})
	b := site("claude-sonnet-5", scan.ArchetypeChat, scan.Shape{})
	ignored := site("claude-sonnet-5", scan.ArchetypeChat, scan.Shape{})
	ignored.Ignored = true
	unknown := scan.Site{File: "x.ts", Line: 1, Ref: "mystery-9000"}
	total := engine.TotalMonthlyUSD(&scan.Report{Sites: []scan.Site{a, b, ignored, unknown}}, cat)
	// Each sonnet site: (500 input at $2 + 300 output at $10) per Mtok
	// at 10,000 calls is $40/mo. Ignored sites still spend; unknown
	// strings cannot be priced.
	if total != 120 {
		t.Errorf("total = %g, want 120", total)
	}
}

func TestDuplicateCallSites(t *testing.T) {
	engine, cat := loadEngine(t)
	a := site("claude-sonnet-5", scan.ArchetypeChat, scan.Shape{})
	b := site("claude-sonnet-5", scan.ArchetypeChat, scan.Shape{})
	c := site("claude-sonnet-5", scan.ArchetypeChat, scan.Shape{})
	a.Line, b.Line, c.Line = 3, 9, 21
	b.File, c.File = "twin.ts", "twin.ts"
	a.Hash, b.Hash, c.Hash = "feedbeef", "feedbeef", "feedbeef"
	got := engine.Evaluate(&scan.Report{Sites: []scan.Site{a, b, c}}, cat)
	if len(got) != 2 {
		t.Fatalf("got %+v, want findings on the two later duplicates only", got)
	}
	for i, want := range []struct {
		file string
		line int
	}{{"twin.ts", 9}, {"twin.ts", 21}} {
		f := got[i]
		if f.RuleID != "duplicate-call-sites" || f.File != want.file || f.Line != want.line {
			t.Errorf("finding %d = %s at %s:%d, want duplicate-call-sites at %s:%d",
				i, f.RuleID, f.File, f.Line, want.file, want.line)
		}
		if f.Confidence != "low" {
			t.Errorf("confidence = %s, want low", f.Confidence)
		}
		if f.CandidateText != "consolidate with the first identical call and share one cached result" {
			t.Errorf("candidate = %q", f.CandidateText)
		}
	}
}

func TestDuplicatesNeedSameModel(t *testing.T) {
	engine, cat := loadEngine(t)
	a := site("claude-sonnet-5", scan.ArchetypeChat, scan.Shape{})
	b := site("claude-haiku-4-5", scan.ArchetypeChat, scan.Shape{})
	a.Hash, b.Hash = "feedbeef", "feedbeef"
	if got := engine.Evaluate(&scan.Report{Sites: []scan.Site{a, b}}, cat); len(got) != 0 {
		t.Errorf("got %+v, want no findings for the same hash on different models", got)
	}
}

func TestDuplicatesSkipIgnoredAndHashless(t *testing.T) {
	engine, cat := loadEngine(t)
	a := site("claude-sonnet-5", scan.ArchetypeChat, scan.Shape{})
	b := site("claude-sonnet-5", scan.ArchetypeChat, scan.Shape{})
	a.Hash, b.Hash = "feedbeef", "feedbeef"
	a.Ignored = true
	empty1 := site("claude-haiku-4-5", scan.ArchetypeChat, scan.Shape{})
	empty2 := site("claude-haiku-4-5", scan.ArchetypeChat, scan.Shape{})
	empty1.Hash, empty2.Hash = "", ""
	if got := engine.Evaluate(&scan.Report{Sites: []scan.Site{a, b, empty1, empty2}}, cat); len(got) != 0 {
		t.Errorf("got %+v, want no findings when the twin is ignored or hashes are empty", got)
	}
}

// A fallback chain needs no rule of its own: layer 2 reports every
// model string as a site, so the retired entry in the array draws
// deprecated-model at its own line.
func TestFallbackChainRetiredModel(t *testing.T) {
	engine, cat := loadEngine(t)
	dir := t.TempDir()
	src := `const PRIMARY = "gpt-5.1";
const FALLBACK_MODELS = ["gpt-5.1", "text-davinci-003"];
`
	if err := os.WriteFile(filepath.Join(dir, "models.js"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	report, err := scan.Analyze(dir, cat)
	if err != nil {
		t.Fatal(err)
	}
	got := engine.Evaluate(report, cat)
	if len(got) != 1 {
		t.Fatalf("got %+v, want exactly the deprecated-model finding", got)
	}
	f := got[0]
	if f.RuleID != "deprecated-model" || f.File != "models.js" || f.Line != 2 {
		t.Errorf("finding = %s at %s:%d, want deprecated-model at models.js:2", f.RuleID, f.File, f.Line)
	}
	if f.CandidateModel != "gpt-5-mini" {
		t.Errorf("candidate model = %q, want the active successor gpt-5-mini", f.CandidateModel)
	}
}

// batch-on-realtime excludes only embeddings, so transcription on a
// cron job rides the existing batch rule.
func TestTranscriptionOnCron(t *testing.T) {
	engine, cat := loadEngine(t)
	report := &scan.Report{Sites: []scan.Site{
		site("gpt-4o-mini", scan.ArchetypeTranscription, scan.Shape{BatchContext: true}),
	}}
	got := engine.Evaluate(report, cat)
	if len(got) != 1 || got[0].RuleID != "batch-on-realtime" {
		t.Fatalf("got %+v, want one batch-on-realtime finding", got)
	}
	want := "gpt-4o-mini through the batch endpoint at half price"
	if len(got[0].CandidateText) < len(want) || got[0].CandidateText[:len(want)] != want {
		t.Errorf("candidate = %q, want it to open with %q", got[0].CandidateText, want)
	}
}

// Below a read fraction of 1.0 the remaining system tokens are charged
// at the cache write rate. Estimates are built locally; the shipped
// fraction is 1.0, which zeroes the write term.
func TestCachedMonthlyUSDWriteRate(t *testing.T) {
	e := &Engine{}
	e.Est.Volume.CallsPerMonth = 10000
	e.Est.Tokens.CharsPerToken = 4
	e.Est.Tokens.DefaultInput = 500
	e.Est.Tokens.DefaultOutput = 400
	e.Est.Tokens.EmbeddingInput = 10000
	e.Est.Cache.SteadyStateReadFraction = 0.8
	m := &catalog.Model{
		ID: "cached-model", InputPerMtok: 3, OutputPerMtok: 15,
		CacheReadPerMtok: 0.3, CacheWritePerMtok: 3.75,
	}
	s := scan.Site{Archetype: scan.ArchetypeChat, Shape: scan.Shape{SystemPromptChars: 8000}}
	// 2,000 system tokens: 1,600 read at $0.30, 400 written at $3.75,
	// plus 500 input at $3 and 400 output at $15, at 10,000 calls:
	// (1500 + 480 + 1500 + 6000) / 1e6 x 10000 = $94.80.
	got := e.cachedMonthlyUSD(m, s, 10000)
	if math.Abs(got-94.8) > 1e-9 {
		t.Errorf("cachedMonthlyUSD = %g, want 94.8", got)
	}
	// At fraction 1.0 the write term vanishes; the difference is the
	// write rate's contribution.
	e.Est.Cache.SteadyStateReadFraction = 1
	if allRead := e.cachedMonthlyUSD(m, s, 10000); math.Abs(allRead-81) > 1e-9 {
		t.Errorf("cachedMonthlyUSD at fraction 1.0 = %g, want 81", allRead)
	}
}

// pricey-embeddings needs a cheaper same provider embedding to name.
// mistral-embed and gemini-embedding-001 are their providers' only
// ones, and cohere's alternative costs more. The openai pair still
// nominates.
func TestNoCheaperEmbedding(t *testing.T) {
	engine, cat := loadEngine(t)
	report := &scan.Report{Sites: []scan.Site{
		site("mistral-embed", scan.ArchetypeEmbedding, scan.Shape{}),
		site("gemini-embedding-001", scan.ArchetypeEmbedding, scan.Shape{Dimensions: intPtr(256)}),
		site("embed-english-v3.0", scan.ArchetypeEmbedding, scan.Shape{}),
	}}
	for _, f := range engine.Evaluate(report, cat) {
		if f.RuleID == "pricey-embeddings" {
			t.Errorf("pricey-embeddings fired on %s with no cheaper sibling to name", f.Model)
		}
	}
	report = &scan.Report{Sites: []scan.Site{
		site("text-embedding-3-large", scan.ArchetypeEmbedding, scan.Shape{Dimensions: intPtr(256)}),
	}}
	got := engine.Evaluate(report, cat)
	if len(got) != 1 || got[0].RuleID != "pricey-embeddings" || got[0].CandidateModel != "text-embedding-3-small" {
		t.Errorf("got %+v, want pricey-embeddings nominating text-embedding-3-small", got)
	}
}

// price_multiplier prefers the model's own batch_multiplier; the yaml
// multiplier is the fallback for entries without one. Both are 0.5 in
// shipped data, so this needs a synthetic catalog.
func TestPriceMultiplierPrefersModel(t *testing.T) {
	engine, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	entry := catalog.Model{
		Provider: "testco", InputPerMtok: 2, OutputPerMtok: 10,
		ContextWindow: 100000, Tier: "mid", Released: "2025-01-01",
		Source: "https://example.com/pricing",
	}
	discounted := entry
	discounted.ID = "deep-discount"
	discounted.BatchMultiplier = 0.4
	flat := entry
	flat.ID = "no-discount"
	cat := &catalog.Catalog{Version: "2026-01-01", Models: []catalog.Model{discounted, flat}}
	report := &scan.Report{Sites: []scan.Site{
		site("deep-discount", scan.ArchetypeSummarization, scan.Shape{BatchContext: true}),
		site("no-discount", scan.ArchetypeSummarization, scan.Shape{BatchContext: true}),
	}}
	got := engine.Evaluate(report, cat)
	if len(got) != 2 {
		t.Fatalf("got %+v, want two batch-on-realtime findings", got)
	}
	// Each site runs (500 in at $2 + 300 out at $10) x 10,000 calls =
	// $40/mo; 0.4 of that is $16, the 0.5 yaml fallback gives $20.
	byModel := map[string]string{}
	for _, f := range got {
		if f.RuleID != "batch-on-realtime" {
			t.Fatalf("finding = %+v, want batch-on-realtime", f)
		}
		byModel[f.Model] = f.CandidateText
	}
	if want := "deep-discount through the batch endpoint at half price, ~$16/mo"; byModel["deep-discount"] != want {
		t.Errorf("candidate = %q, want %q (model's 0.4 multiplier)", byModel["deep-discount"], want)
	}
	if want := "no-discount through the batch endpoint at half price, ~$20/mo"; byModel["no-discount"] != want {
		t.Errorf("candidate = %q, want %q (yaml 0.5 fallback)", byModel["no-discount"], want)
	}
}

// Cohere's other active embedding, embed-v4.0, is pricier, so nominate
// falls back to the no-candidate wording.
func TestNominateNeverRaisesCost(t *testing.T) {
	engine, cat := loadEngine(t)
	current := cat.ByName("embed-english-v3.0")
	if current == nil {
		t.Fatal("embed-english-v3.0 is missing from the catalog")
	}
	s := site("embed-english-v3.0", scan.ArchetypeEmbedding, scan.Shape{})
	text, model := engine.nominate(cat, current, "embedding", "same provider at the standard embedding tier", s, 10000)
	if model != "" {
		t.Fatalf("nominated %q, want no candidate when every sibling costs more", model)
	}
	if text != "no active embedding tier model from cohere in the catalog" {
		t.Errorf("text = %q, want the no-candidate fallback wording", text)
	}
	// The guard rejects raises, not downgrades: a pricier current model
	// still draws its cheaper sibling.
	pricier := cat.ByName("text-embedding-3-large")
	_, model = engine.nominate(cat, pricier, "embedding", "same provider at the standard embedding tier", s, 10000)
	if model != "text-embedding-3-small" {
		t.Errorf("nominated %q, want text-embedding-3-small for text-embedding-3-large", model)
	}
}

// A rule that leans on the archetype inherits the classifier's doubt.
func TestLowConfidenceDemotes(t *testing.T) {
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
