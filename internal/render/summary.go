package render

import (
	"fmt"

	"github.com/MithrilBytes/overwater/rules"
)

// SummaryLine renders the whole verdict as a single line for shell
// prompts and CI logs, without a trailing newline.
func SummaryLine(findings []rules.Finding, meta Meta) string {
	if len(findings) == 0 {
		return fmt.Sprintf("overwater: keep the models you have (catalog %s)", meta.CatalogVersion)
	}
	total := 0
	for _, f := range findings {
		total += f.MonthlyUSD
	}
	return fmt.Sprintf("overwater: %d findings, ~$%s/mo estimated at %s calls/mo, catalog %s",
		len(findings), comma(total), comma(meta.CallsPerMonth), meta.CatalogVersion)
}
