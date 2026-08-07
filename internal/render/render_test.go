package render

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/MithrilBytes/overwater/rules"
)

func sampleFinding() rules.Finding {
	return rules.Finding{
		RuleID:        "frontier-extraction",
		Confidence:    "high",
		File:          "src/tagger.ts",
		Line:          41,
		Archetype:     "extraction",
		Evidence:      "temp 0, JSON schema",
		Model:         "claude-fable-5",
		MonthlyUSD:    340,
		CandidateText: "claude-haiku-4-5, same capability tier for this task class, ~$19/mo",
		Tripwire:      "If eval agreement drops below 97%, stay put",
	}
}

func TestBlockFieldOrder(t *testing.T) {
	f := sampleFinding()
	f.Flags = []string{"No prompt caching on a 1,800-token repeated system prompt"}
	got := block(f)
	want := "Call site: src/tagger.ts:41 (extraction: temp 0, JSON schema; high confidence)\n" +
		"Current:   claude-fable-5 at ~$340/mo at estimated volume\n" +
		"Candidate: claude-haiku-4-5, same capability tier for this task class, ~$19/mo\n" +
		"Tripwire:  If eval agreement drops below 97%, stay put\n" +
		"Flag:      No prompt caching on a 1,800-token repeated system prompt\n"
	if got != want {
		t.Errorf("block mismatch\n got: %q\nwant: %q", got, want)
	}
}

func TestBlockNoFlags(t *testing.T) {
	if got := block(sampleFinding()); !strings.Contains(got, "Flag:      None\n") {
		t.Errorf("block = %q, want a None flag line", got)
	}
}

func TestTerminalNullVerdict(t *testing.T) {
	var out bytes.Buffer
	Terminal(&out, nil, Meta{CatalogVersion: "2026-08-05", CallsPerMonth: 10000})
	want := "Prices from catalog 2026-08-05.\n\nKeep the models you have.\n"
	if out.String() != want {
		t.Errorf("terminal = %q, want %q", out.String(), want)
	}
}

func TestJSONFindingsArray(t *testing.T) {
	out, err := JSON(nil, Meta{CatalogVersion: "2026-08-05", CallsPerMonth: 10000})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), `"findings": []`) {
		t.Errorf("JSON = %s, want an empty findings array, not null", out)
	}
	var parsed struct {
		CatalogVersion string `json:"catalog_version"`
		CallsPerMonth  int    `json:"calls_per_month"`
	}
	if err := json.Unmarshal(out, &parsed); err != nil {
		t.Fatal(err)
	}
	if parsed.CatalogVersion != "2026-08-05" || parsed.CallsPerMonth != 10000 {
		t.Errorf("parsed = %+v", parsed)
	}
}

func TestJSONCarriesCandidateModel(t *testing.T) {
	f := sampleFinding()
	f.CandidateModel = "claude-haiku-4-5"
	out, err := JSON([]rules.Finding{f}, Meta{CatalogVersion: "2026-08-05", CallsPerMonth: 10000})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), `"candidate_model": "claude-haiku-4-5"`) {
		t.Errorf("JSON = %s, want the candidate model as its own field", out)
	}
}

// Provenance reaches the reader: a measured number never reads as an
// estimate, and a number from anywhere else always does.
func TestBlockSaysWhereTheVolumeCameFrom(t *testing.T) {
	cases := map[string]string{
		"measured": "at ~$340/mo at measured volume",
		"pragma":   "at ~$340/mo at estimated volume",
		"config":   "at ~$340/mo at estimated volume",
		"flag":     "at ~$340/mo at estimated volume",
		"estimate": "at ~$340/mo at estimated volume",
		"":         "at ~$340/mo at estimated volume",
	}
	for source, want := range cases {
		f := sampleFinding()
		f.VolumeSource = source
		if got := block(f); !strings.Contains(got, want) {
			t.Errorf("source %q: block = %q, want it to contain %q", source, got, want)
		}
	}
}

func TestHeaderCountsMeasuredSites(t *testing.T) {
	meta := Meta{CatalogVersion: "2026-08-05", CallsPerMonth: 10000}
	measured, estimated := sampleFinding(), sampleFinding()
	measured.VolumeSource = "measured"
	estimated.VolumeSource = "estimate"
	cases := []struct {
		name     string
		findings []rules.Finding
		want     string
	}{
		{"none measured", []rules.Finding{estimated}, "Costs are estimates at 10,000 calls per"},
		{"all measured", []rules.Finding{measured}, "Costs use measured volumes for every"},
		{"mixed", []rules.Finding{measured, estimated}, "measured volumes for 1 of 2"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			Terminal(&out, tc.findings, meta)
			if !strings.Contains(out.String(), tc.want) {
				t.Errorf("header = %q, want it to contain %q", out.String(), tc.want)
			}
		})
	}
}

func TestJSONCarriesVolumeProvenance(t *testing.T) {
	f := sampleFinding()
	f.Volume = 250000
	f.VolumeSource = "measured"
	out, err := JSON([]rules.Finding{f}, Meta{CatalogVersion: "2026-08-05", CallsPerMonth: 10000})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"volume": 250000`, `"volume_source": "measured"`} {
		if !strings.Contains(string(out), want) {
			t.Errorf("JSON = %s, want %s", out, want)
		}
	}
}

func TestComma(t *testing.T) {
	cases := map[int]string{0: "0", 999: "999", 1000: "1,000", 10000: "10,000", 1234567: "1,234,567"}
	for n, want := range cases {
		if got := comma(n); got != want {
			t.Errorf("comma(%d) = %q, want %q", n, got, want)
		}
	}
}
