package rules

import (
	"fmt"

	"github.com/MithrilBytes/overwater/catalog"
	"github.com/MithrilBytes/overwater/internal/scan"
)

// nominate picks the provider's newest active model in the target tier
// and renders the candidate clause. Newest, not cheapest: once the
// catalog carries history, cheapest would nominate stale bargains
// instead of the lane's current occupant. Every caller is a cost
// motivated strategy, so a candidate whose monthly cost for this site
// exceeds the current model's is rejected outright; a savings
// nomination must never raise the bill. When no model survives, the
// finding says so instead of guessing.
func (e *Engine) nominate(cat *catalog.Catalog, current *catalog.Model, tier, note string, site scan.Site) (string, string) {
	budget := e.monthlyUSD(current, site)
	var best *catalog.Model
	for i := range cat.Models {
		m := &cat.Models[i]
		if m.Provider != current.Provider || m.Tier != tier || m.ID == current.ID || m.Deprecated != "" {
			continue
		}
		if e.monthlyUSD(m, site) > budget {
			continue
		}
		if best == nil || newer(m, best) {
			best = m
		}
	}
	if best == nil {
		return fmt.Sprintf("no active %s tier model from %s in the catalog", tier, current.Provider), ""
	}
	cost := round(e.monthlyUSD(best, site))
	return fmt.Sprintf("%s, %s, ~$%d/mo", best.ID, note, cost), best.ID
}

// newer ranks candidates: later release first, then lower price, then id
// for a stable order.
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
