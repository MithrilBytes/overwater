// Package rules maps archetype plus shape plus catalog tier to findings.
// Every rule, threshold, and price lives in the embedded YAML files; the
// engine contains no numbers of its own. Findings are nominations with a
// stated confidence, never directives.
package rules

import (
	"bytes"
	"embed"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/MithrilBytes/overwater/catalog"
	"github.com/MithrilBytes/overwater/internal/scan"
)

//go:embed *.yaml
var ruleFiles embed.FS

// Estimates holds the cost model assumptions from estimates.yaml.
type Estimates struct {
	Volume struct {
		CallsPerMonth int `yaml:"calls_per_month"`
	} `yaml:"volume"`
	Tokens struct {
		CharsPerToken  int `yaml:"chars_per_token"`
		DefaultInput   int `yaml:"default_input"`
		DefaultOutput  int `yaml:"default_output"`
		EmbeddingInput int `yaml:"embedding_input"`
	} `yaml:"tokens"`
	Cache struct {
		SteadyStateReadFraction float64 `yaml:"steady_state_read_fraction"`
	} `yaml:"cache"`
}

// When is the predicate side of a rule. Absent fields do not constrain.
type When struct {
	Archetype         []string `yaml:"archetype"`
	ArchetypeNot      []string `yaml:"archetype_not"`
	Tier              []string `yaml:"tier"`
	Provider          []string `yaml:"provider"`
	Deprecated        *bool    `yaml:"deprecated"`
	BatchContext      *bool    `yaml:"batch_context"`
	BatchAPI          *bool    `yaml:"batch_api"`
	ShapeReadable     *bool    `yaml:"shape_readable"`
	MaxTokensPresent  *bool    `yaml:"max_tokens_present"`
	CacheControl      *bool    `yaml:"cache_control"`
	MinSystemTokens   int      `yaml:"min_system_tokens"`
	MinInputPerMtok   float64  `yaml:"min_input_per_mtok"`
	Effort            []string `yaml:"effort"`
	MinRetries        int      `yaml:"min_retries"`
	TemperatureAbove  *float64 `yaml:"temperature_above"`
	ImageDetailHigh   *bool    `yaml:"image_detail_high"`
	ModelCapability   string   `yaml:"model_capability"`
	DimensionsPresent *bool    `yaml:"dimensions_present"`
	// MinDuplicateSites is the cross site predicate: the site's hash and
	// model appear at least this many times in the report, and the site
	// is not the first occurrence. The engine computes the grouping.
	MinDuplicateSites int `yaml:"min_duplicate_sites"`
}

// Candidate describes how a rule nominates an alternative.
type Candidate struct {
	Strategy   string  `yaml:"strategy"`
	Tier       string  `yaml:"tier"`
	Multiplier float64 `yaml:"multiplier"`
	Note       string  `yaml:"note"`
}

var candidateStrategies = map[string]bool{
	"none": true, "price_multiplier": true, "tier_downgrade": true,
	"successor": true, "cheapest_embedding": true, "cached_system_prompt": true,
}

// Rule is one data file. Kind finding produces a verdict block of its
// own; kind flag attaches a line to the finding at the same call site,
// or becomes a finding itself when the site has no other.
type Rule struct {
	ID         string    `yaml:"id"`
	Kind       string    `yaml:"kind"`
	Confidence string    `yaml:"confidence"`
	When       When      `yaml:"when"`
	Candidate  Candidate `yaml:"candidate"`
	Tripwire   string    `yaml:"tripwire"`
	Flag       string    `yaml:"flag"`
}

// Finding is one downgrade nomination for one call site. SiteHash is
// the scanner's content hash of the call site, the drift stable part of
// the baseline fingerprint.
type Finding struct {
	RuleID        string
	Confidence    string
	File          string
	Line          int
	SiteHash      string
	Archetype     string
	Evidence      string
	Model         string
	MonthlyUSD    int
	CandidateText string // the full clause rendered after "Candidate:"
	// CandidateModel is the nominated model id when the candidate is a
	// different model, empty for same model or shape only candidates.
	// The eval generator keys off it.
	CandidateModel string
	Tripwire       string
	Flags          []string
}

// Engine evaluates the loaded rules against a scan report.
type Engine struct {
	Rules []Rule
	Est   Estimates
}

// Load reads the embedded rule and estimate files.
func Load() (*Engine, error) {
	entries, err := ruleFiles.ReadDir(".")
	if err != nil {
		return nil, err
	}
	e := &Engine{}
	for _, entry := range entries {
		raw, err := ruleFiles.ReadFile(entry.Name())
		if err != nil {
			return nil, err
		}
		dec := yaml.NewDecoder(bytes.NewReader(raw))
		dec.KnownFields(true)
		if entry.Name() == "estimates.yaml" {
			if err := dec.Decode(&e.Est); err != nil {
				return nil, fmt.Errorf("%s: %w", entry.Name(), err)
			}
			continue
		}
		var r Rule
		if err := dec.Decode(&r); err != nil {
			return nil, fmt.Errorf("%s: %w", entry.Name(), err)
		}
		if err := r.validate(); err != nil {
			return nil, fmt.Errorf("%s: %w", entry.Name(), err)
		}
		e.Rules = append(e.Rules, r)
	}
	if e.Est.Volume.CallsPerMonth <= 0 || e.Est.Tokens.CharsPerToken <= 0 {
		return nil, fmt.Errorf("estimates.yaml is missing volume or token assumptions")
	}
	if f := e.Est.Cache.SteadyStateReadFraction; f <= 0 || f > 1 {
		return nil, fmt.Errorf("estimates.yaml cache.steady_state_read_fraction must be in (0, 1]")
	}
	return e, nil
}

func (r Rule) validate() error {
	if r.ID == "" {
		return fmt.Errorf("rule is missing an id")
	}
	if r.Kind != "finding" && r.Kind != "flag" {
		return fmt.Errorf("%s: kind must be finding or flag", r.ID)
	}
	if r.Confidence != "low" && r.Confidence != "medium" && r.Confidence != "high" {
		return fmt.Errorf("%s: confidence must be low, medium, or high", r.ID)
	}
	if !candidateStrategies[r.Candidate.Strategy] {
		return fmt.Errorf("%s: unknown candidate strategy %q", r.ID, r.Candidate.Strategy)
	}
	if r.Candidate.Note == "" || r.Tripwire == "" {
		return fmt.Errorf("%s: candidate note and tripwire are required", r.ID)
	}
	return nil
}

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
			siteFindings = append(siteFindings, e.finding(r, site, model, cat))
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
		cost := round(current * r.Candidate.Multiplier)
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

// nominate picks the provider's newest active model in the target tier
// and renders the candidate clause. Newest, not cheapest: once the
// catalog carries history, cheapest would nominate stale bargains
// instead of the lane's current occupant. When the catalog has no such
// model, the finding says so instead of guessing.
func (e *Engine) nominate(cat *catalog.Catalog, current *catalog.Model, tier, note string, site scan.Site) (string, string) {
	var best *catalog.Model
	for i := range cat.Models {
		m := &cat.Models[i]
		if m.Provider != current.Provider || m.Tier != tier || m.ID == current.ID || m.Deprecated != "" {
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

func round(x float64) int {
	return int(math.Round(x))
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
