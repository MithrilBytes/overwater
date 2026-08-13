package catalog

import (
	"reflect"
	"strings"
	"testing"
)

func TestBareIDStripsRouting(t *testing.T) {
	for key, want := range map[string]string{
		"azure_ai/deepseek-v4-flash":      "deepseek-v4-flash",
		"dashscope/deepseek-v4-flash":     "deepseek-v4-flash",
		"us.anthropic.claude-opus-4-7":    "claude-opus-4-7",
		"global.anthropic.claude-fable-5": "claude-fable-5",
		"gemini/gemini-3-flash-preview":   "gemini-3-flash-preview",
		"gpt-4o":                          "gpt-4o",
		// A host we have not named still yields the last segment.
		"fireworks_ai/accounts/fireworks/models/qwen3-vl": "qwen3-vl",
	} {
		if got := bareID(key); got != want {
			t.Errorf("bareID(%q) = %q, want %q", key, got, want)
		}
	}
}

func TestReverseDiffSkipsWhatWeCarry(t *testing.T) {
	m := validModel()
	m.ID = "test-model"
	m.Aliases = []string{"test-model-latest"}
	c := &Catalog{Version: "2026-01-01", Models: []Model{m}}

	prices := LitellmPrices{
		// Ours, under three routes. None may be reported.
		"test-model":              {Input: 1, Output: 2, HasOutput: true, Mode: "chat"},
		"azure_ai/test-model":     {Input: 1, Output: 2, HasOutput: true, Mode: "chat"},
		"us.anthropic.test-model": {Input: 1, Output: 2, HasOutput: true, Mode: "chat"},
		// Not ours, under two routes: one entry, two keys.
		"brand-new-model":         {Input: 3, Output: 6, HasOutput: true, MaxInput: 128000, Mode: "chat", Provider: "acme"},
		"bedrock/brand-new-model": {Input: 3, Output: 6, HasOutput: true, Mode: "chat", Provider: "bedrock"},
	}

	got := ReverseDiff(c, prices, nil)
	if len(got) != 1 {
		t.Fatalf("unlisted = %+v, want only brand-new-model", got)
	}
	if got[0].ID != "brand-new-model" || len(got[0].Keys) != 2 {
		t.Errorf("entry = %+v, want brand-new-model collapsed from two routes", got[0])
	}
	if got[0].MaxInput != 128000 {
		t.Errorf("max input = %d, want the route that carried one", got[0].MaxInput)
	}
}

// Image and audio models price per asset, and nothing in the rules
// engine reasons about them.
func TestReverseDiffKeepsOnlyTokenModes(t *testing.T) {
	c := &Catalog{Version: "2026-01-01"}
	prices := LitellmPrices{
		"a-chat":  {Input: 1, Mode: "chat"},
		"a-embed": {Input: 1, Mode: "embedding"},
		"a-image": {Input: 1, Mode: "image_generation"},
		"a-audio": {Input: 1, Mode: "audio_transcription"},
	}
	got := ReverseDiff(c, prices, nil)
	if len(got) != 2 {
		t.Fatalf("unlisted = %+v, want the chat and embedding entries only", got)
	}
}

// Fourteen upstream keys list prices a thousandfold high. Reporting them
// first would bury the answer under models nobody sells.
func TestReverseDiffRejectsImpossiblePrices(t *testing.T) {
	c := &Catalog{Version: "2026-01-01"}
	prices := LitellmPrices{
		"sane":     {Input: 75, Output: 150, HasOutput: true, Mode: "chat"},
		"nonsense": {Input: 135000, Output: 540000, HasOutput: true, Mode: "chat"},
	}
	got := ReverseDiff(c, prices, nil)
	if len(got) != 1 || got[0].ID != "sane" {
		t.Fatalf("unlisted = %+v, want the sane entry only", got)
	}
}

// The narrowing a scan's unrecognised models feed into.
func TestReverseDiffNarrowsToRequestedIDs(t *testing.T) {
	c := &Catalog{Version: "2026-01-01"}
	prices := LitellmPrices{
		"wanted":          {Input: 1, Mode: "chat"},
		"azure_ai/wanted": {Input: 1, Mode: "chat"},
		"unwanted":        {Input: 9, Mode: "chat"},
	}
	// The id arrives however the source spelled it, routing and all.
	got := ReverseDiff(c, prices, []string{"bedrock/WANTED"})
	if len(got) != 1 || got[0].ID != "wanted" {
		t.Fatalf("unlisted = %+v, want only wanted", got)
	}
}

func TestReverseDiffOrdersByPrice(t *testing.T) {
	c := &Catalog{Version: "2026-01-01"}
	prices := LitellmPrices{
		"cheap":  {Input: 0.1, Mode: "chat"},
		"dear":   {Input: 30, Mode: "chat"},
		"middle": {Input: 3, Mode: "chat"},
	}
	got := ReverseDiff(c, prices, nil)
	want := []string{"dear", "middle", "cheap"}
	for i, w := range want {
		if got[i].ID != w {
			t.Fatalf("order = %v, want %v", []Unlisted{got[0], got[1], got[2]}, want)
		}
	}
}

// The nightly job diffs today's roster against the one committed
// yesterday, so the same upstream file has to produce the same list
// every time. It did not: Unlisted.ID took its spelling from whichever
// of a model's routes Go's map iteration reached first, and a roster
// that reshuffles reports every model as new, nightly. Dropping three
// entries from a real 1,281 model roster reported thirty-four new.
func TestReverseDiffIsDeterministic(t *testing.T) {
	c := &Catalog{Version: "2026-01-01"}
	prices := LitellmPrices{}
	// Many models, each under several routes that spell it differently.
	for _, id := range []string{"Alpha-One", "beta-two", "Gamma-3", "delta-4", "Epsilon-5"} {
		for _, route := range []string{
			"", "azure_ai/", "bedrock/", "us.anthropic.", "vertex_ai/", "novita/",
		} {
			prices[route+id] = LitellmEntry{Input: 1, Output: 2, HasOutput: true, Mode: "chat"}
			prices[route+strings.ToLower(id)] = LitellmEntry{Input: 1, Output: 2, HasOutput: true, Mode: "chat"}
		}
	}

	first := ReverseDiff(c, prices, nil)
	for i := 0; i < 20; i++ {
		got := ReverseDiff(c, prices, nil)
		if !reflect.DeepEqual(first, got) {
			t.Fatalf("run %d differs:\nfirst: %+v\n  got: %+v", i, first, got)
		}
	}
	// Each model collapsed to one entry despite twelve routes apiece.
	if len(first) != 5 {
		t.Errorf("entries = %d, want 5: %+v", len(first), first)
	}
}
