package render

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"strings"
	"testing"

	"github.com/MithrilBytes/overwater/rules"
)

func TestSummaryLine(t *testing.T) {
	meta := Meta{CatalogVersion: "2026-08-05", CallsPerMonth: 10000}
	second := sampleFinding()
	second.MonthlyUSD = 900
	cases := []struct {
		name     string
		findings []rules.Finding
		want     string
	}{
		{
			name:     "findings",
			findings: []rules.Finding{sampleFinding(), second},
			want:     "overwater: 2 findings, ~$1,240/mo estimated at 10,000 calls/mo, catalog 2026-08-05",
		},
		{
			name:     "none",
			findings: nil,
			want:     "overwater: keep the models you have (catalog 2026-08-05)",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := SummaryLine(tc.findings, meta); got != tc.want {
				t.Errorf("SummaryLine = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestCSVHeaderAndQuoting(t *testing.T) {
	out := string(CSV([]rules.Finding{sampleFinding()}))
	header := "rule,confidence,file,line,archetype,evidence,model,monthly_usd,candidate,candidate_model,tripwire,flags\n"
	if !strings.HasPrefix(out, header) {
		t.Errorf("CSV = %q, want it to start with the header row", out)
	}
	quoted := `"claude-haiku-4-5, same capability tier for this task class, ~$19/mo"`
	if !strings.Contains(out, quoted) {
		t.Errorf("CSV = %q, want the comma bearing candidate quoted as %q", out, quoted)
	}
	if !strings.HasSuffix(out, "\n") {
		t.Errorf("CSV = %q, want a trailing newline", out)
	}
}

// Cells the repo controls must never open as formulas in a spreadsheet
// app: a leading =, +, -, @, tab, or carriage return gets a quote prefix.
func TestCSVNeutralizesFormulaLeadingChars(t *testing.T) {
	leads := []string{
		`=HYPERLINK("http://evil.example","click")`,
		"+cmd|' /C calc'!A0",
		"-2+3+cmd",
		"@SUM(A1:A9)",
		"\tstill=formula",
		"\rstill=formula",
	}
	var findings []rules.Finding
	for _, lead := range leads {
		f := sampleFinding()
		f.File = lead
		f.CandidateText = lead
		findings = append(findings, f)
	}
	out := CSV(findings)
	rows, err := csv.NewReader(bytes.NewReader(out)).ReadAll()
	if err != nil {
		t.Fatalf("output is not valid CSV: %v", err)
	}
	cells := map[string]bool{}
	for _, row := range rows[1:] {
		for _, cell := range row {
			cells[cell] = true
			if cell == "" {
				continue
			}
			switch cell[0] {
			case '=', '+', '-', '@', '\t', '\r':
				t.Errorf("cell %q still starts with a formula character", cell)
			}
		}
	}
	for _, lead := range leads {
		if !cells["'"+lead] {
			t.Errorf("no cell carries the neutralized %q:\n%s", lead, out)
		}
	}
	// Benign cells stay byte identical: the sample's own fields carry no
	// leading formula characters and must gain no quote.
	plain := string(CSV([]rules.Finding{sampleFinding()}))
	if strings.Contains(plain, "'") {
		t.Errorf("benign CSV gained a quote prefix: %q", plain)
	}
}

func TestHTMLFindings(t *testing.T) {
	f := sampleFinding()
	f.Flags = []string{"No prompt caching on a 1,800-token repeated system prompt"}
	out := string(HTML([]rules.Finding{f}, Meta{CatalogVersion: "2026-08-05", CallsPerMonth: 10000}))
	for _, label := range []string{"Call site", "Current", "Candidate", "Tripwire", "Flag"} {
		if !strings.Contains(out, ">"+label+"<") {
			t.Errorf("HTML is missing the %q label", label)
		}
	}
	for _, want := range []string{"claude-fable-5", "catalog 2026-08-05", "10,000 calls"} {
		if !strings.Contains(out, want) {
			t.Errorf("HTML is missing %q", want)
		}
	}
	if strings.Contains(out, "http") {
		t.Error("HTML references an external asset")
	}
}

func TestHTMLNullVerdict(t *testing.T) {
	out := string(HTML(nil, Meta{CatalogVersion: "2026-08-05", CallsPerMonth: 10000}))
	if !strings.Contains(out, KeepVerdict) {
		t.Errorf("HTML = %q, want the keep verdict sentence", out)
	}
	if strings.Contains(out, "http") {
		t.Error("HTML references an external asset")
	}
}

func TestSARIFLog(t *testing.T) {
	high := sampleFinding()
	medium := sampleFinding()
	medium.RuleID = "cron-batch"
	medium.Confidence = "medium"
	medium.File = "jobs/summarize.js"
	medium.Line = 7
	out, err := SARIF([]rules.Finding{high, medium}, Meta{CatalogVersion: "2026-08-05", CallsPerMonth: 10000})
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Schema  string `json:"$schema"`
		Version string `json:"version"`
		Runs    []struct {
			Tool struct {
				Driver struct {
					Name  string `json:"name"`
					Rules []struct {
						ID string `json:"id"`
					} `json:"rules"`
				} `json:"driver"`
			} `json:"tool"`
			Results []struct {
				RuleID    string `json:"ruleId"`
				Level     string `json:"level"`
				Locations []struct {
					PhysicalLocation struct {
						ArtifactLocation struct {
							URI string `json:"uri"`
						} `json:"artifactLocation"`
						Region struct {
							StartLine int `json:"startLine"`
						} `json:"region"`
					} `json:"physicalLocation"`
				} `json:"locations"`
			} `json:"results"`
		} `json:"runs"`
	}
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatal(err)
	}
	if doc.Version != "2.1.0" {
		t.Errorf("version = %q, want 2.1.0", doc.Version)
	}
	if !strings.Contains(doc.Schema, "sarif-schema-2.1.0.json") {
		t.Errorf("$schema = %q, want the 2.1.0 schema", doc.Schema)
	}
	if len(doc.Runs) != 1 {
		t.Fatalf("runs = %d, want 1", len(doc.Runs))
	}
	run := doc.Runs[0]
	if run.Tool.Driver.Name != "overwater" {
		t.Errorf("driver name = %q, want overwater", run.Tool.Driver.Name)
	}
	if len(run.Tool.Driver.Rules) != 2 {
		t.Errorf("driver rules = %d, want 2 distinct entries", len(run.Tool.Driver.Rules))
	}
	if len(run.Results) != 2 {
		t.Fatalf("results = %d, want 2", len(run.Results))
	}
	levels := []struct {
		index int
		want  string
	}{
		{0, "warning"},
		{1, "note"},
	}
	for _, tc := range levels {
		if got := run.Results[tc.index].Level; got != tc.want {
			t.Errorf("results[%d].level = %q, want %q", tc.index, got, tc.want)
		}
	}
	loc := run.Results[1].Locations[0].PhysicalLocation
	if loc.ArtifactLocation.URI != "jobs/summarize.js" || loc.Region.StartLine != 7 {
		t.Errorf("location = %s:%d, want jobs/summarize.js:7", loc.ArtifactLocation.URI, loc.Region.StartLine)
	}
}
