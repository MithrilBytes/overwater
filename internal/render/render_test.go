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

func TestBlockRendersFiveFieldsInOrder(t *testing.T) {
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

func TestBlockWithoutFlagsSaysNone(t *testing.T) {
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

func TestJSONFindingsAlwaysAnArray(t *testing.T) {
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

func TestCommaFormatting(t *testing.T) {
	cases := map[int]string{0: "0", 999: "999", 1000: "1,000", 10000: "10,000", 1234567: "1,234,567"}
	for n, want := range cases {
		if got := comma(n); got != want {
			t.Errorf("comma(%d) = %q, want %q", n, got, want)
		}
	}
}
