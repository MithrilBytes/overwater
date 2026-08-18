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
	fmt.Fprint(w, costHeader(findings, meta))
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
	b.WriteString(costHeader(findings, meta))
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
	// tripwireCheck is the tripwire as numbers: what an eval measures,
	// which way the comparison runs, and the bar. Absent on findings
	// whose tripwire names nothing an eval can measure.
	type tripwireCheck struct {
		Metric    string  `json:"metric"`
		Compare   string  `json:"compare"`
		Threshold float64 `json:"threshold"`
	}
	type jsonFinding struct {
		Rule           string         `json:"rule"`
		Confidence     string         `json:"confidence"`
		File           string         `json:"file"`
		Line           int            `json:"line"`
		Archetype      string         `json:"archetype"`
		Evidence       string         `json:"evidence,omitempty"`
		Model          string         `json:"model"`
		MonthlyUSD     int            `json:"monthly_usd"`
		Volume         int            `json:"volume"`
		VolumeSource   string         `json:"volume_source"`
		Callers        int            `json:"callers,omitempty"`
		Candidate      string         `json:"candidate"`
		CandidateModel string         `json:"candidate_model,omitempty"`
		Tripwire       string         `json:"tripwire"`
		TripwireCheck  *tripwireCheck `json:"tripwire_check,omitempty"`
		Flags          []string       `json:"flags,omitempty"`
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
		var check *tripwireCheck
		if c := f.TripwireCheck; c.Set() {
			check = &tripwireCheck{Metric: c.Metric, Compare: c.Compare, Threshold: c.Threshold}
		}
		report.Findings = append(report.Findings, jsonFinding{
			Rule:           f.RuleID,
			Confidence:     f.Confidence,
			File:           f.File,
			Line:           f.Line,
			Archetype:      f.Archetype,
			Evidence:       f.Evidence,
			Model:          f.Model,
			MonthlyUSD:     f.MonthlyUSD,
			Volume:         f.Volume,
			VolumeSource:   volumeSource(f),
			Callers:        f.Callers,
			Candidate:      f.CandidateText,
			CandidateModel: f.CandidateModel,
			Tripwire:       f.Tripwire,
			TripwireCheck:  check,
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
	fmt.Fprintf(&b, "Call site: %s:%d (%s; %s confidence)\n", escapePath(f.File), f.Line, head, f.Confidence)
	fmt.Fprintf(&b, "Current:   %s\n", current(f))
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

// escapePath defuses the one string on a finding the scanned repo
// controls. A POSIX filename may hold ESC, newlines and backticks, so a
// bare path can drive the terminal cursor or close the MODELS.md fence
// and write a verdict of the repo's choosing; percent escaping those
// bytes leaves an ordinary path byte for byte unchanged.
func escapePath(p string) string {
	var b strings.Builder
	for i := 0; i < len(p); i++ {
		if c := p[i]; c < 0x20 || c == 0x7f || c == '`' {
			fmt.Fprintf(&b, "%%%02X", c)
		} else {
			b.WriteByte(c)
		}
	}
	return b.String()
}

// current is the "Current:" sentence every surface shares. A fan-in
// volume names the callers it covers, so a figure many times the per
// site estimate says where the multiplier came from.
func current(f rules.Finding) string {
	s := fmt.Sprintf("%s at ~$%d/mo at %s volume", f.Model, f.MonthlyUSD, volumeWord(f))
	if volumeSource(f) == rules.VolumeFanIn && f.Callers > 1 {
		s += fmt.Sprintf(" across %d callers", f.Callers)
	}
	return s
}

// volumeSource is the finding's provenance, reading an unset source as
// the estimate default.
func volumeSource(f rules.Finding) string {
	if f.VolumeSource == "" {
		return rules.VolumeEstimate
	}
	return f.VolumeSource
}

// volumeWord is the one word every surface uses to say whether a dollar
// figure rests on real traffic. Only a volumes file reads as measured;
// a pragma, a config volume, and --volume are all assumptions.
func volumeWord(f rules.Finding) string {
	if volumeSource(f) == rules.VolumeMeasured {
		return "measured"
	}
	return "estimated"
}

func measuredCount(findings []rules.Finding) int {
	n := 0
	for _, f := range findings {
		if volumeSource(f) == rules.VolumeMeasured {
			n++
		}
	}
	return n
}

// costHeader states the assumption behind the dollars: the default
// estimate, measured traffic, or the mix of both.
func costHeader(findings []rules.Finding, meta Meta) string {
	measured, total := measuredCount(findings), len(findings)
	switch {
	case measured == 0:
		return fmt.Sprintf(
			"Prices from catalog %s. Costs are estimates at %s calls per\nmonth per call site; override with --volume.\n",
			meta.CatalogVersion, comma(meta.CallsPerMonth))
	case measured == total:
		return fmt.Sprintf(
			"Prices from catalog %s. Costs use measured volumes for every\ncall site.\n",
			meta.CatalogVersion)
	default:
		return fmt.Sprintf(
			"Prices from catalog %s. Costs use measured volumes for %d of %d\ncall sites; the rest are estimates at %s calls per month.\n",
			meta.CatalogVersion, measured, total, comma(meta.CallsPerMonth))
	}
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
