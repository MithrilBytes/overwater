package rules

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/MithrilBytes/overwater/catalog"
	"github.com/MithrilBytes/overwater/internal/scan"
)

// dupKey groups call sites for the duplicate predicate: same content
// hash and same model is the same call written twice.
type dupKey struct{ hash, model string }

// Evaluate runs every rule against every known call site and returns the
// findings sorted by file, line, and rule id.
func (e *Engine) Evaluate(report *scan.Report, cat *catalog.Catalog) []Finding {
	names := cat.Names()
	counts := map[dupKey]int{}
	for _, site := range report.Sites {
		if site.Known && !site.Ignored && site.Hash != "" && names[site.Ref] != nil {
			counts[dupKey{site.Hash, site.Ref}]++
		}
	}
	seen := map[dupKey]int{}
	var findings []Finding
	for _, site := range report.Sites {
		if !site.Known || site.Ignored {
			continue
		}
		model := names[site.Ref]
		if model == nil {
			continue
		}
		var dupCount, dupPos int
		if site.Hash != "" {
			k := dupKey{site.Hash, site.Ref}
			dupCount, dupPos = counts[k], seen[k]
			seen[k]++
		}
		var siteFindings []Finding
		var flagRules []Rule
		for _, r := range e.Rules {
			if !e.matches(r.When, site, model, dupCount, dupPos) {
				continue
			}
			if r.Kind == "flag" {
				flagRules = append(flagRules, r)
				continue
			}
			f := e.finding(r, site, model, cat)
			// cheapest_embedding exists solely to name a cheaper same
			// provider sibling. When nothing cheaper exists, a finding
			// would only restate the price with no move to make, so it
			// is dropped instead of shipped with the no-candidate
			// wording.
			if r.Candidate.Strategy == "cheapest_embedding" && f.CandidateModel == "" {
				continue
			}
			siteFindings = append(siteFindings, f)
		}
		if len(siteFindings) == 0 {
			// A flag with no host finding still deserves a verdict block.
			for _, r := range flagRules {
				siteFindings = append(siteFindings, e.finding(r, site, model, cat))
			}
		} else {
			for _, r := range flagRules {
				siteFindings[0].Flags = append(siteFindings[0].Flags, e.template(r.Flag, site, model))
			}
		}
		findings = append(findings, siteFindings...)
	}
	sort.Slice(findings, func(i, j int) bool {
		a, b := findings[i], findings[j]
		if a.File != b.File {
			return a.File < b.File
		}
		if a.Line != b.Line {
			return a.Line < b.Line
		}
		return a.RuleID < b.RuleID
	})
	return findings
}

func (e *Engine) matches(w When, site scan.Site, m *catalog.Model, dupCount, dupPos int) bool {
	if len(w.Archetype) > 0 && !contains(w.Archetype, site.Archetype) {
		return false
	}
	if contains(w.ArchetypeNot, site.Archetype) {
		return false
	}
	if len(w.Tier) > 0 && !contains(w.Tier, m.Tier) {
		return false
	}
	if len(w.Provider) > 0 && !contains(w.Provider, m.Provider) {
		return false
	}
	if w.Deprecated != nil && (m.Deprecated != "") != *w.Deprecated {
		return false
	}
	if w.BatchContext != nil && site.Shape.BatchContext != *w.BatchContext {
		return false
	}
	if w.BatchAPI != nil && site.Shape.BatchAPI != *w.BatchAPI {
		return false
	}
	if w.ShapeReadable != nil && site.Shape.Readable != *w.ShapeReadable {
		return false
	}
	if w.MaxTokensPresent != nil && (site.Shape.MaxTokens != nil) != *w.MaxTokensPresent {
		return false
	}
	if w.CacheControl != nil && site.Shape.CacheControl != *w.CacheControl {
		return false
	}
	if w.MinSystemTokens > 0 && e.systemTokens(site) < w.MinSystemTokens {
		return false
	}
	if w.MinInputPerMtok > 0 && m.InputPerMtok < w.MinInputPerMtok {
		return false
	}
	if len(w.Effort) > 0 && !contains(w.Effort, site.Shape.Effort) {
		return false
	}
	if w.MinRetries > 0 && (site.Shape.MaxRetries == nil || *site.Shape.MaxRetries < w.MinRetries) {
		return false
	}
	if w.TemperatureAbove != nil && (site.Shape.Temperature == nil || *site.Shape.Temperature <= *w.TemperatureAbove) {
		return false
	}
	if w.ImageDetailHigh != nil && site.Shape.ImageDetailHigh != *w.ImageDetailHigh {
		return false
	}
	if w.ModelCapability != "" && !m.HasCapability(w.ModelCapability) {
		return false
	}
	if w.DimensionsPresent != nil && (site.Shape.Dimensions != nil) != *w.DimensionsPresent {
		return false
	}
	if w.MinDuplicateSites > 0 && (dupCount < w.MinDuplicateSites || dupPos == 0) {
		return false
	}
	return true
}

func (e *Engine) finding(r Rule, site scan.Site, m *catalog.Model, cat *catalog.Catalog) Finding {
	current := e.monthlyUSD(m, site)
	confidence := r.Confidence
	// A rule that leans on the archetype inherits the classifier's
	// doubt: a low confidence classification demotes the finding one
	// notch instead of presenting a guess as certainty.
	if (len(r.When.Archetype) > 0 || len(r.When.ArchetypeNot) > 0) && site.ArchetypeConfidence == "low" {
		confidence = demote(confidence)
	}
	f := Finding{
		RuleID:     r.ID,
		Confidence: confidence,
		File:       site.File,
		Line:       site.Line,
		SiteHash:   site.Hash,
		Archetype:  site.Archetype,
		Evidence:   evidence(site),
		Model:      m.ID,
		MonthlyUSD: round(current),
		Tripwire:   r.Tripwire,
	}
	if r.Flag != "" {
		f.Flags = append(f.Flags, e.template(r.Flag, site, m))
	}
	switch r.Candidate.Strategy {
	case "none":
		f.CandidateText = r.Candidate.Note
	case "cached_system_prompt":
		// Priced only when the catalog knows the model's cache rates;
		// otherwise the nomination stays a shape suggestion.
		f.CandidateText = r.Candidate.Note
		if m.CacheReadPerMtok > 0 {
			f.CandidateText = fmt.Sprintf("%s, ~$%d/mo", r.Candidate.Note, round(e.cachedMonthlyUSD(m, site)))
		}
	case "price_multiplier":
		// The model's own published batch discount beats the rule's
		// flat assumption; the yaml multiplier is the fallback for
		// entries that do not carry one.
		mult := r.Candidate.Multiplier
		if m.BatchMultiplier > 0 {
			mult = m.BatchMultiplier
		}
		cost := round(current * mult)
		f.CandidateText = fmt.Sprintf("%s %s, ~$%d/mo", m.ID, r.Candidate.Note, cost)
	case "tier_downgrade":
		f.CandidateText, f.CandidateModel = e.nominate(cat, m, r.Candidate.Tier, r.Candidate.Note, site)
	case "successor":
		f.CandidateText, f.CandidateModel = e.nominate(cat, m, m.Tier, r.Candidate.Note, site)
	case "cheapest_embedding":
		f.CandidateText, f.CandidateModel = e.nominate(cat, m, "embedding", r.Candidate.Note, site)
	}
	return f
}

// evidence renders the visible signals for the call site line, in a
// fixed order so output stays deterministic.
func evidence(site scan.Site) string {
	if site.Archetype == scan.ArchetypeEmbedding {
		return "embeddings API"
	}
	var parts []string
	if site.Shape.Temperature != nil {
		parts = append(parts, fmt.Sprintf("temp %g", *site.Shape.Temperature))
	}
	if site.Shape.JSONSchema {
		parts = append(parts, "JSON schema")
	}
	if site.Shape.Streaming {
		parts = append(parts, "streaming")
	}
	if site.Shape.BatchContext {
		parts = append(parts, "cron scheduled")
	}
	if site.Shape.Readable && site.Shape.MaxTokens == nil {
		parts = append(parts, "no max_tokens")
	}
	return strings.Join(parts, ", ")
}

func (e *Engine) template(tpl string, site scan.Site, m *catalog.Model) string {
	s := strings.ReplaceAll(tpl, "{system_tokens}", comma(e.systemTokens(site)))
	if site.Shape.MaxRetries != nil {
		s = strings.ReplaceAll(s, "{max_retries}", strconv.Itoa(*site.Shape.MaxRetries))
	}
	return strings.ReplaceAll(s, "{deprecated_date}", m.Deprecated)
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

func demote(confidence string) string {
	switch confidence {
	case "high":
		return "medium"
	default:
		return "low"
	}
}
