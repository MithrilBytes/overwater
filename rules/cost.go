package rules

import (
	"math"

	"github.com/MithrilBytes/overwater/catalog"
	"github.com/MithrilBytes/overwater/internal/scan"
)

// TotalMonthlyUSD sums the estimated monthly spend of every known call
// site at its own model, for the repo budget check. Ignore pragmas
// silence findings, not spend, so ignored sites still count.
func (e *Engine) TotalMonthlyUSD(report *scan.Report, cat *catalog.Catalog) float64 {
	names := cat.Names()
	var total float64
	for _, site := range report.Sites {
		if !site.Known {
			continue
		}
		if m := names[site.Ref]; m != nil {
			total += e.monthlyUSD(m, site)
		}
	}
	return total
}

// monthlyUSD estimates the monthly spend for one call site on one model,
// using only the assumptions in estimates.yaml.
func (e *Engine) monthlyUSD(m *catalog.Model, site scan.Site) float64 {
	t := e.Est.Tokens
	var in, out int
	if site.Archetype == scan.ArchetypeEmbedding {
		in, out = t.EmbeddingInput, 0
	} else {
		in = t.DefaultInput + site.Shape.SystemPromptChars/t.CharsPerToken
		out = t.DefaultOutput
		if site.Shape.MaxTokens != nil && *site.Shape.MaxTokens < out {
			out = *site.Shape.MaxTokens
		}
	}
	perCall := (float64(in)*m.InputPerMtok + float64(out)*m.OutputPerMtok) / 1e6
	return perCall * float64(e.callsPerMonth(site))
}

// cachedMonthlyUSD prices the same call with the system prompt served
// from the provider cache: the steady state fraction of system tokens
// at the cache read rate, the rest at the write rate, everything else
// unchanged. Callers gate on the model actually publishing cache rates.
func (e *Engine) cachedMonthlyUSD(m *catalog.Model, site scan.Site) float64 {
	t := e.Est.Tokens
	sys := float64(e.systemTokens(site))
	out := t.DefaultOutput
	if site.Shape.MaxTokens != nil && *site.Shape.MaxTokens < out {
		out = *site.Shape.MaxTokens
	}
	read := sys * e.Est.Cache.SteadyStateReadFraction
	write := sys - read
	perCall := (float64(t.DefaultInput)*m.InputPerMtok +
		read*m.CacheReadPerMtok + write*m.CacheWritePerMtok +
		float64(out)*m.OutputPerMtok) / 1e6
	return perCall * float64(e.callsPerMonth(site))
}

// callsPerMonth is the effective volume for one site: a volume pragma
// wins over the estimate default.
func (e *Engine) callsPerMonth(site scan.Site) int {
	if site.VolumeOverride > 0 {
		return site.VolumeOverride
	}
	return e.Est.Volume.CallsPerMonth
}

func (e *Engine) systemTokens(site scan.Site) int {
	return site.Shape.SystemPromptChars / e.Est.Tokens.CharsPerToken
}

func round(x float64) int {
	return int(math.Round(x))
}
