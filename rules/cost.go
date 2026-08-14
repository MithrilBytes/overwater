package rules

import (
	"math"

	"github.com/MithrilBytes/overwater/catalog"
	"github.com/MithrilBytes/overwater/internal/scan"
)

// TotalMonthlyUSD sums the monthly spend of every known call site at
// its own model, for the repo budget check. Ignored sites still count:
// ignore pragmas silence findings, not spend. Volumes resolve as they
// do in Evaluate, so the budget and the report agree.
func (e *Engine) TotalMonthlyUSD(report *scan.Report, cat *catalog.Catalog) float64 {
	names := cat.Names()
	vols := e.bindVolumes(report, cat)
	var total float64
	for _, site := range report.Sites {
		if !site.Known {
			continue
		}
		if m := names[site.Ref]; m != nil {
			total += e.monthlyUSD(m, site, vols.forSite(site, m).calls)
		}
	}
	return total
}

// monthlyUSD prices one call site on one model at the given monthly
// call count, using only the assumptions in estimates.yaml.
func (e *Engine) monthlyUSD(m *catalog.Model, site scan.Site, calls int) float64 {
	t := e.Est.Tokens
	var in, out int
	if site.Archetype == scan.ArchetypeEmbedding {
		in, out = t.EmbeddingInput, 0
	} else {
		in = t.DefaultInput + site.Shape.SystemPromptChars/t.CharsPerToken
		out = e.outputTokens(site)
	}
	perCall := (float64(in)*m.InputPerMtok + float64(out)*m.OutputPerMtok) / 1e6
	return perCall * float64(calls)
}

// cachedMonthlyUSD prices the same call with the system prompt served
// from the provider cache: the steady state fraction of system tokens
// at the cache read rate, the rest at the write rate. Callers gate on
// the model publishing cache rates at all.
func (e *Engine) cachedMonthlyUSD(m *catalog.Model, site scan.Site, calls int) float64 {
	t := e.Est.Tokens
	sys := float64(e.systemTokens(site))
	out := e.outputTokens(site)
	read := sys * e.Est.Cache.SteadyStateReadFraction
	write := sys - read
	perCall := (float64(t.DefaultInput)*m.InputPerMtok +
		read*m.CacheReadPerMtok + write*m.CacheWritePerMtok +
		float64(out)*m.OutputPerMtok) / 1e6
	return perCall * float64(calls)
}

func (e *Engine) systemTokens(site scan.Site) int {
	return site.Shape.SystemPromptChars / e.Est.Tokens.CharsPerToken
}

// outputTokens is the default output assumption lowered by whatever the
// call site proves it cannot exceed: a response schema the model must
// fill in, and an explicit cap. Nothing here raises the estimate; a
// wide schema or a generous max_tokens leaves default_output standing.
func (e *Engine) outputTokens(site scan.Site) int {
	out := e.Est.Tokens.DefaultOutput
	if b := e.schemaBound(site.Shape); b > 0 && b < out {
		out = b
	}
	if site.Shape.MaxTokens != nil && *site.Shape.MaxTokens < out {
		out = *site.Shape.MaxTokens
	}
	return out
}

// schemaBound is the widest response the schema at this site can hold.
// Only the two shapes the reader is sure of bound anything: every field
// an enum, or several fields that are not. A lone free text field is
// neither, since one "summary": string can run as long as the model
// likes. 0 means the schema says nothing about length.
func (e *Engine) schemaBound(s scan.Shape) int {
	if s.SchemaFields <= 0 {
		return 0
	}
	b := e.Est.Tokens.SchemaOutput
	switch {
	case s.SchemaEnumOnly:
		return b.Envelope + b.PerEnumField*s.SchemaFields
	case s.SchemaMultiField:
		return b.Envelope + b.PerField*s.SchemaFields
	}
	return 0
}

func round(x float64) int {
	return int(math.Round(x))
}
