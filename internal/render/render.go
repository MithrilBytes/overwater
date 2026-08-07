// Package render turns findings into each output surface: terminal text
// for humans, MODELS.md for the scanned repo, JSON, CSV, SARIF, and HTML
// for everything else. Renderers only format; every fact arrives
// precomputed on the finding.
package render

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/MithrilBytes/overwater/rules"
)

// Meta carries the scan level facts every renderer states.
type Meta struct {
	CatalogVersion string
	CallsPerMonth  int
}

// KeepVerdict is the null verdict, rendered verbatim when nothing fires.
const KeepVerdict = "Keep the models you have."

// Terminal writes the human readable verdict.
func Terminal(w io.Writer, findings []rules.Finding, meta Meta) {
	if len(findings) == 0 {
		fmt.Fprint(w, nullHeader(meta))
		return
	}
	fmt.Fprint(w, costHeader(meta))
	for _, f := range findings {
		fmt.Fprint(w, "\n"+block(f))
	}
}

// ModelsMD renders the verdict as the MODELS.md file written into the
// scanned repo. Its output must reproduce the goldens byte for byte.
func ModelsMD(findings []rules.Finding, meta Meta) []byte {
	var b bytes.Buffer
	b.WriteString("# Overwater verdict\n\n")
	if len(findings) == 0 {
		b.WriteString(nullHeader(meta))
		return b.Bytes()
	}
	b.WriteString(costHeader(meta))
	for _, f := range findings {
		b.WriteString("\n```\n")
		b.WriteString(block(f))
		b.WriteString("```\n")
	}
	return b.Bytes()
}

// JSON renders the findings for machines. The findings field is always
// an array, never null.
func JSON(findings []rules.Finding, meta Meta) ([]byte, error) {
	type jsonFinding struct {
		Rule           string   `json:"rule"`
		Confidence     string   `json:"confidence"`
		File           string   `json:"file"`
		Line           int      `json:"line"`
		Archetype      string   `json:"archetype"`
		Evidence       string   `json:"evidence,omitempty"`
		Model          string   `json:"model"`
		MonthlyUSD     int      `json:"monthly_usd"`
		Candidate      string   `json:"candidate"`
		CandidateModel string   `json:"candidate_model,omitempty"`
		Tripwire       string   `json:"tripwire"`
		Flags          []string `json:"flags,omitempty"`
	}
	report := struct {
		CatalogVersion string        `json:"catalog_version"`
		CallsPerMonth  int           `json:"calls_per_month"`
		Findings       []jsonFinding `json:"findings"`
	}{
		CatalogVersion: meta.CatalogVersion,
		CallsPerMonth:  meta.CallsPerMonth,
		Findings:       make([]jsonFinding, 0, len(findings)),
	}
	for _, f := range findings {
		report.Findings = append(report.Findings, jsonFinding{
			Rule:           f.RuleID,
			Confidence:     f.Confidence,
			File:           f.File,
			Line:           f.Line,
			Archetype:      f.Archetype,
			Evidence:       f.Evidence,
			Model:          f.Model,
			MonthlyUSD:     f.MonthlyUSD,
			Candidate:      f.CandidateText,
			CandidateModel: f.CandidateModel,
			Tripwire:       f.Tripwire,
			Flags:          f.Flags,
		})
	}
	out, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(out, '\n'), nil
}

// block renders the verdict contract for one finding, in the fixed
// order: Call site, Current, Candidate, Tripwire, Flag.
func block(f rules.Finding) string {
	var b strings.Builder
	head := f.Archetype
	if f.Evidence != "" {
		head += ": " + f.Evidence
	}
	fmt.Fprintf(&b, "Call site: %s:%d (%s; %s confidence)\n", f.File, f.Line, head, f.Confidence)
	fmt.Fprintf(&b, "Current:   %s at ~$%d/mo at estimated volume\n", f.Model, f.MonthlyUSD)
	fmt.Fprintf(&b, "Candidate: %s\n", f.CandidateText)
	fmt.Fprintf(&b, "Tripwire:  %s\n", f.Tripwire)
	if len(f.Flags) == 0 {
		b.WriteString("Flag:      None\n")
	} else {
		for _, flag := range f.Flags {
			fmt.Fprintf(&b, "Flag:      %s\n", flag)
		}
	}
	return b.String()
}

func costHeader(meta Meta) string {
	return fmt.Sprintf(
		"Prices from catalog %s. Costs are estimates at %s calls per\nmonth per call site; override with --volume.\n",
		meta.CatalogVersion, comma(meta.CallsPerMonth))
}

func nullHeader(meta Meta) string {
	return fmt.Sprintf("Prices from catalog %s.\n\n%s\n", meta.CatalogVersion, KeepVerdict)
}

func comma(n int) string {
	s := strconv.Itoa(n)
	if len(s) <= 3 {
		return s
	}
	var b strings.Builder
	pre := len(s) % 3
	if pre > 0 {
		b.WriteString(s[:pre])
	}
	for i := pre; i < len(s); i += 3 {
		if b.Len() > 0 {
			b.WriteByte(',')
		}
		b.WriteString(s[i : i+3])
	}
	return b.String()
}
