package rules

import (
	"testing"

	"github.com/MithrilBytes/overwater/catalog"
	"github.com/MithrilBytes/overwater/internal/scan"
)

// tierPair builds a two entry catalog: a frontier model that declares
// caps, and one small tier candidate. Shipped entries move under the
// price watch, so the capability rules need entries the test owns.
func tierPair(caps []string, candidate catalog.Model) *catalog.Catalog {
	current := catalog.Model{
		ID: "big", Provider: "testco", InputPerMtok: 2, OutputPerMtok: 10,
		ContextWindow: 100000, Tier: "frontier", Released: "2025-01-01",
		Source: "https://example.com/pricing", Capabilities: caps,
	}
	return &catalog.Catalog{Version: "2026-01-01", Models: []catalog.Model{current, candidate}}
}

func smallEntry(id, released string, caps []string) catalog.Model {
	return catalog.Model{
		ID: id, Provider: "testco", InputPerMtok: 0.1, OutputPerMtok: 0.4,
		ContextWindow: 100000, Tier: "small", Released: released,
		Source: "https://example.com/pricing", Capabilities: caps,
	}
}

// A downgrade must not trade a capability away. The newest small tier
// entry here declares no vision, and an extraction site that sends an
// image fails on it whatever the saving says.
func TestNominateSkipsCandidateMissingCapability(t *testing.T) {
	engine, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	blind := smallEntry("small-blind", "2025-06-01", []string{"tools"})
	seeing := smallEntry("small-seeing", "2025-03-01", []string{"vision", "tools"})
	cat := tierPair([]string{"vision", "tools"}, blind)
	cat.Models = append(cat.Models, seeing)
	s := site("big", scan.ArchetypeExtraction, scan.Shape{})
	text, model := engine.nominate(cat, &cat.Models[0], "small", "same capability tier for this task class", s, 10000)
	if model != "small-seeing" {
		t.Errorf("nominated %q (%s), want small-seeing: small-blind is newer but declares no vision", model, text)
	}
}

// Most of the catalog declares no capabilities at all, so an empty
// list is missing data rather than a claim of no features. Requiring a
// superset of it would leave those providers with nothing to nominate.
func TestNominateAllowsCandidateWithoutCapabilityData(t *testing.T) {
	engine, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	cat := tierPair([]string{"vision", "tools"}, smallEntry("small-quiet", "2025-06-01", nil))
	s := site("big", scan.ArchetypeExtraction, scan.Shape{})
	_, model := engine.nominate(cat, &cat.Models[0], "small", "same capability tier for this task class", s, 10000)
	if model != "small-quiet" {
		t.Errorf("nominated %q, want small-quiet: neither entry has capabilities to compare", model)
	}
}
