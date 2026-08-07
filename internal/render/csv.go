package render

import (
	"bytes"
	"encoding/csv"
	"strconv"
	"strings"

	"github.com/MithrilBytes/overwater/rules"
)

// CSV renders one row per finding for spreadsheets. Multiple flags share
// one cell joined with "; "; the csv package handles RFC 4180 quoting.
func CSV(findings []rules.Finding) []byte {
	var b bytes.Buffer
	w := csv.NewWriter(&b)
	w.Write([]string{
		"rule", "confidence", "file", "line", "archetype", "evidence",
		"model", "monthly_usd", "candidate", "candidate_model", "tripwire", "flags",
	})
	for _, f := range findings {
		w.Write([]string{
			neutralize(f.RuleID),
			neutralize(f.Confidence),
			neutralize(f.File),
			strconv.Itoa(f.Line),
			neutralize(f.Archetype),
			neutralize(f.Evidence),
			neutralize(f.Model),
			strconv.Itoa(f.MonthlyUSD),
			neutralize(f.CandidateText),
			neutralize(f.CandidateModel),
			neutralize(f.Tripwire),
			neutralize(strings.Join(f.Flags, "; ")),
		})
	}
	w.Flush()
	return b.Bytes()
}

// neutralize defuses spreadsheet formula injection. File paths, model
// strings, and evidence are repo controlled, and a cell starting with
// =, +, -, @, tab, or carriage return executes as a formula when the
// CSV opens in a spreadsheet app. A leading single quote keeps it text.
func neutralize(cell string) string {
	if cell == "" {
		return cell
	}
	switch cell[0] {
	case '=', '+', '-', '@', '\t', '\r':
		return "'" + cell
	}
	return cell
}
