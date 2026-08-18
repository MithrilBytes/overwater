package rules

import (
	"fmt"

	"github.com/MithrilBytes/overwater/catalog"
	"github.com/MithrilBytes/overwater/internal/scan"
)

// nominate picks the provider's newest active model in the target tier
// and renders the candidate clause. Newest, not cheapest: cheapest
// names stale bargains once the catalog carries history. A candidate
// costing more than the current model at this site is rejected; with
// none left the clause says so. The tier is the one the rule names and
// nothing else, so a cheaper model one tier over is unreachable even
// though the budget guard below would make it safe to consider.
func (e *Engine) nominate(cat *catalog.Catalog, current *catalog.Model, tier, note string, site scan.Site, calls int) (string, string) {
	budget := e.monthlyUSD(current, site, calls)
	var best *catalog.Model
	for i := range cat.Models {
		m := &cat.Models[i]
		if m.Provider != current.Provider || m.Tier != tier || m.ID == current.ID || m.Deprecated != "" {
			continue
		}
		if !keepsCapabilities(m, current) {
			continue
		}
		if e.monthlyUSD(m, site, calls) > budget {
			continue
		}
		if best == nil || newer(m, best) {
			best = m
		}
	}
	if best == nil {
		return fmt.Sprintf("no active %s tier model from %s in the catalog", tier, current.Provider), ""
	}
	cost := round(e.monthlyUSD(best, site, calls))
	return fmt.Sprintf("%s, %s, ~$%d/mo", best.ID, note, cost), best.ID
}

// keepsCapabilities rejects a candidate that does not declare
// everything the current model does. A vision site moved to a model
// without vision fails on its first image, which costs more than the
// bill it saves. An empty list is missing data, not a claim of no
// features, so an entry declaring nothing on either side stands
// outside the comparison rather than emptying its tier.
func keepsCapabilities(candidate, current *catalog.Model) bool {
	if len(candidate.Capabilities) == 0 || len(current.Capabilities) == 0 {
		return true
	}
	for _, c := range current.Capabilities {
		if !candidate.HasCapability(c) {
			return false
		}
	}
	return true
}

// newer ranks candidates: later release, then lower price, then id for
// a stable order.
func newer(a, b *catalog.Model) bool {
	if a.Released != b.Released {
		return a.Released > b.Released
	}
	if pricePoint(a) != pricePoint(b) {
		return pricePoint(a) < pricePoint(b)
	}
	return a.ID < b.ID
}

func pricePoint(m *catalog.Model) float64 {
	return m.InputPerMtok + m.OutputPerMtok
}
