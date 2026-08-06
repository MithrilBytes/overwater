package catalog

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const sampleLitellm = `{
  "test-model": {"input_cost_per_token": 0.000002, "output_cost_per_token": 0.000008, "litellm_provider": "testco", "max_input_tokens": 9000},
  "testco/aliased-model": {"input_cost_per_token": 0.0000005, "output_cost_per_token": 0.000001},
  "sample_spec": {"input_cost_per_token": "not a number"}
}`

func TestParseLitellmConvertsPerTokenToPerMillion(t *testing.T) {
	prices, err := ParseLitellm([]byte(sampleLitellm))
	if err != nil {
		t.Fatal(err)
	}
	p, ok := prices["test-model"]
	if !ok || p.Input != 2 || p.Output != 8 {
		t.Fatalf("test-model = %+v, want 2 and 8 per million", p)
	}
	if _, ok := prices["sample_spec"]; ok {
		t.Error("malformed entry should be skipped")
	}
}

func diffFixtureCatalog() *Catalog {
	drifted := validModel()
	drifted.ID = "test-model"
	matching := validModel()
	matching.ID = "aliased-model"
	matching.Aliases = []string{"aliased-latest"}
	matching.InputPerMtok = 0.5
	matching.OutputPerMtok = 1
	absent := validModel()
	absent.ID = "not-tracked"
	retired := validModel()
	retired.ID = "old-model"
	retired.Deprecated = "2025-01-01"
	return &Catalog{Version: "2026-01-01", Models: []Model{drifted, matching, absent, retired}}
}

func TestDiffLitellmMatchesAndTolerates(t *testing.T) {
	prices, err := ParseLitellm([]byte(sampleLitellm))
	if err != nil {
		t.Fatal(err)
	}
	drifts, notes, missing := DiffLitellm(diffFixtureCatalog(), prices)
	if len(drifts) != 1 || drifts[0].ID != "test-model" {
		t.Fatalf("drifts = %+v, want only test-model", drifts)
	}
	if drifts[0].TheirsIn != 2 || drifts[0].TheirsOut != 8 {
		t.Errorf("drift prices = %+v", drifts[0])
	}
	if len(notes) != 1 || !strings.Contains(notes[0], "context window") {
		t.Errorf("notes = %v, want the context window disagreement reported", notes)
	}
	// aliased-model matched via the provider prefixed key and its
	// prices agree, so it is neither drifted nor missing; the retired
	// entry is skipped entirely.
	if len(missing) != 1 || missing[0] != "not-tracked" {
		t.Errorf("missing = %v, want only not-tracked", missing)
	}
}

func TestApplyPricesRewritesBumpsAndRebuilds(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "models"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "VERSION"), []byte("2026-01-01\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	entry := "id: test-model\nprovider: testco\ninput_per_mtok: 1\noutput_per_mtok: 2\ncontext_window: 1000\ntier: mid\nreleased: \"2025-01-01\"\nsource: https://example.com/pricing\n"
	if err := os.WriteFile(filepath.Join(dir, "models", "test-model.yaml"), []byte(entry), 0o644); err != nil {
		t.Fatal(err)
	}
	drift := Drift{ID: "test-model", TheirsIn: 2, TheirsOut: 8}
	if err := ApplyPrices(dir, []Drift{drift}, "2026-08-05"); err != nil {
		t.Fatal(err)
	}
	updated, err := os.ReadFile(filepath.Join(dir, "models", "test-model.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(updated), "input_per_mtok: 2\n") ||
		!strings.Contains(string(updated), "output_per_mtok: 8\n") {
		t.Errorf("entry not rewritten: %s", updated)
	}
	version, err := os.ReadFile(filepath.Join(dir, "VERSION"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(version)) != "2026-08-05" {
		t.Errorf("VERSION = %q", version)
	}
	rebuilt, err := os.ReadFile(filepath.Join(dir, "catalog.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(rebuilt), `"input_per_mtok": 2`) {
		t.Errorf("catalog.json not rebuilt: %s", rebuilt)
	}
}
