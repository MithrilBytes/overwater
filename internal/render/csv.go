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
			f.RuleID,
			f.Confidence,
			f.File,
			strconv.Itoa(f.Line),
			f.Archetype,
			f.Evidence,
			f.Model,
			strconv.Itoa(f.MonthlyUSD),
			f.CandidateText,
			f.CandidateModel,
			f.Tripwire,
			strings.Join(f.Flags, "; "),
		})
	}
	w.Flush()
	return b.Bytes()
}
