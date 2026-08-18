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

// The path is the only string on a finding the scanned repo writes, and
// a POSIX name may hold newlines, backticks and ESC. Left bare it closes
// the MODELS.md fence to forge a verdict and repaints the terminal, so
// every byte below 0x20 plus 0x7f and the backtick leaves as an escape.
func TestBlockEscapesPathControlBytes(t *testing.T) {
	f := sampleFinding()
	f.File = "a\n```\n\n## Overwater verdict: all clear\n\n```\nb\x1b[2K\x7f.py"
	got := block(f)
	want := "Call site: a%0A%60%60%60%0A%0A## Overwater verdict: all clear%0A%0A%60%60%60%0Ab%1B[2K%7F.py:41 " +
		"(extraction: temp 0, JSON schema; high confidence)\n"
	if !strings.HasPrefix(got, want) {
		t.Errorf("block = %q, want it to start with %q", got, want)
	}
	if lines := strings.Count(got, "\n"); lines != 5 {
		t.Errorf("block = %q, want the 5 fixed lines and no more", got)
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

// The tripwire ships twice: the sentence for a person, and the numbers
// for whatever runs the eval. A finding with nothing measurable to gate
// on carries no object at all rather than an empty one.
func TestJSONCarriesTripwireCheck(t *testing.T) {
	f := sampleFinding()
	f.TripwireCheck = rules.TripwireCheck{Metric: "agreement", Compare: "below", Threshold: 97}
	out, err := JSON([]rules.Finding{f}, Meta{CatalogVersion: "2026-08-05", CallsPerMonth: 10000})
	if err != nil {
		t.Fatal(err)
	}
	var parsed struct {
		Findings []struct {
			Tripwire string `json:"tripwire"`
			Check    *struct {
				Metric    string  `json:"metric"`
				Compare   string  `json:"compare"`
				Threshold float64 `json:"threshold"`
			} `json:"tripwire_check"`
		} `json:"findings"`
	}
	if err := json.Unmarshal(out, &parsed); err != nil {
		t.Fatal(err)
	}
	got := parsed.Findings[0]
	if got.Tripwire != "If eval agreement drops below 97%, stay put" {
		t.Errorf("tripwire = %q, want the prose kept beside the numbers", got.Tripwire)
	}
	if got.Check == nil {
		t.Fatalf("JSON = %s, want a tripwire_check object", out)
	}
	if got.Check.Metric != "agreement" || got.Check.Compare != "below" || got.Check.Threshold != 97 {
		t.Errorf("tripwire_check = %+v", *got.Check)
	}
	if out, _ := JSON([]rules.Finding{sampleFinding()}, Meta{}); strings.Contains(string(out), "tripwire_check") {
		t.Errorf("JSON = %s, want no tripwire_check without one on the finding", out)
	}
}

// Only a volumes file reads as measured; every other source reads as
// an estimate.
func TestBlockVolumeProvenance(t *testing.T) {
	cases := map[string]string{
		"measured": "at ~$340/mo at measured volume",
		"pragma":   "at ~$340/mo at estimated volume",
		"config":   "at ~$340/mo at estimated volume",
		"flag":     "at ~$340/mo at estimated volume",
		"fan-in":   "at ~$340/mo at estimated volume",
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

// The caller count belongs next to the dollar figure it multiplied.
func TestBlockFanInCallers(t *testing.T) {
	f := sampleFinding()
	f.Volume, f.VolumeSource, f.Callers = 80000, rules.VolumeFanIn, 8
	want := "Current:   claude-fable-5 at ~$340/mo at estimated volume across 8 callers\n"
	if got := block(f); !strings.Contains(got, want) {
		t.Errorf("block = %q, want it to contain %q", got, want)
	}
	f.VolumeSource, f.Callers = "estimate", 0
	if got := block(f); strings.Contains(got, "callers") {
		t.Errorf("block = %q, want no caller clause without fan in", got)
	}
}

func TestJSONCarriesFanInCallers(t *testing.T) {
	f := sampleFinding()
	f.Volume, f.VolumeSource, f.Callers = 80000, rules.VolumeFanIn, 8
	out, err := JSON([]rules.Finding{f}, Meta{CatalogVersion: "2026-08-05", CallsPerMonth: 10000})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"volume_source": "fan-in"`, `"callers": 8`} {
		if !strings.Contains(string(out), want) {
			t.Errorf("JSON = %s, want %s", out, want)
		}
	}
	if out, _ := JSON([]rules.Finding{sampleFinding()}, Meta{}); strings.Contains(string(out), "callers") {
		t.Errorf("JSON = %s, want no callers field without fan in", out)
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
