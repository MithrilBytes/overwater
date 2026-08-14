// Package rules maps archetype, call shape, and catalog tier to findings.
// Every rule, threshold, and price lives in the embedded YAML files; no
// numbers belong in this package.
package rules

import (
	"bytes"
	"embed"
	"fmt"
	"slices"

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
		SchemaOutput   struct {
			Envelope     int `yaml:"envelope"`
			PerField     int `yaml:"per_field"`
			PerEnumField int `yaml:"per_enum_field"`
		} `yaml:"schema_output"`
	} `yaml:"tokens"`
	Cache struct {
		SteadyStateReadFraction float64 `yaml:"steady_state_read_fraction"`
	} `yaml:"cache"`
	FanIn struct {
		MultiplyWhen []string `yaml:"multiply_when"`
	} `yaml:"fan_in"`
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
	// MinDuplicateSites matches a site whose hash and model appear at
	// least this many times in the report, and that is not the first
	// occurrence. Evaluate does the grouping.
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

// knownArchetypes is the closed set the scanner can assign. validate
// rejects anything else; it would be a dead predicate.
var knownArchetypes = map[string]bool{
	scan.ArchetypeEmbedding:      true,
	scan.ArchetypeClassification: true,
	scan.ArchetypeExtraction:     true,
	scan.ArchetypeSummarization:  true,
	scan.ArchetypeAgentic:        true,
	scan.ArchetypeChat:           true,
	scan.ArchetypeTranslation:    true,
	scan.ArchetypeReranking:      true,
	scan.ArchetypeModeration:     true,
	scan.ArchetypeTranscription:  true,
	scan.ArchetypeVision:         true,
	scan.ArchetypeCodegen:        true,
	scan.ArchetypeUnknown:        true,
}

// knownFanInStatuses is the closed set the scanner reports for how a
// call site's caller count was established. A typo in
// fan_in.multiply_when would otherwise load fine and price every
// wrapper as a leaf.
var knownFanInStatuses = map[string]bool{
	scan.FanInDirect:     true,
	scan.FanInExact:      true,
	scan.FanInAmbiguous:  true,
	scan.FanInUnresolved: true,
}

// knownEfforts mirrors what the shape reader's effort regex captures,
// lowercased.
var knownEfforts = map[string]bool{
	"minimal": true, "low": true, "medium": true,
	"high": true, "xhigh": true, "max": true,
}

// knownTripwireMetrics is the closed set of numbers a generated eval
// script prints. A metric no script computes would be a gate nobody can
// read. Every one of them is a percentage, which is the bound validate
// holds a threshold to.
var knownTripwireMetrics = map[string]bool{
	"agreement":                  true,
	"nearest_neighbor_agreement": true,
}

// TripwireCheck is the tripwire in the form a generated eval script can
// act on: the number the script measures, which way the comparison
// runs, and the threshold. A rule whose tripwire names nothing an eval
// can measure leaves it unset, and its script prints the sentence
// without gating on it.
type TripwireCheck struct {
	Metric    string  `yaml:"metric"`
	Compare   string  `yaml:"compare"`
	Threshold float64 `yaml:"threshold"`
}

// Set reports whether the rule gave its tripwire a machine readable
// form. The zero check gates nothing.
func (c TripwireCheck) Set() bool { return c.Metric != "" }

func (c TripwireCheck) validate() error {
	if !c.Set() {
		// A threshold with nothing to measure is a number that never
		// fires, which reads as a gate and is not one.
		if c.Compare != "" || c.Threshold != 0 {
			return fmt.Errorf("tripwire_check needs a metric")
		}
		return nil
	}
	if !knownTripwireMetrics[c.Metric] {
		return fmt.Errorf("tripwire_check names unknown metric %q", c.Metric)
	}
	if c.Compare != "below" && c.Compare != "above" {
		return fmt.Errorf("tripwire_check compare must be below or above, got %q", c.Compare)
	}
	if c.Threshold <= 0 || c.Threshold > 100 {
		return fmt.Errorf("tripwire_check threshold is a percentage in (0, 100], got %g", c.Threshold)
	}
	return nil
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
	// Tripwire is the sentence a human reads; TripwireCheck is the same
	// condition as numbers, for the generated eval script to exit on.
	Tripwire      string        `yaml:"tripwire"`
	TripwireCheck TripwireCheck `yaml:"tripwire_check"`
	Flag          string        `yaml:"flag"`
}

// Finding is one downgrade nomination for one call site. SiteHash is
// the scanner's content hash, the drift stable part of the baseline
// fingerprint.
type Finding struct {
	RuleID     string
	Confidence string
	File       string
	Line       int
	SiteHash   string
	Archetype  string
	Evidence   string
	Model      string
	MonthlyUSD int
	// Volume is the calls per month MonthlyUSD was priced at, and
	// VolumeSource is where that number came from: measured, pragma,
	// config, flag, fan-in, or estimate. Callers is how many callers of
	// the enclosing helper the fan-in volume covers, 0 for every other
	// source.
	Volume        int
	VolumeSource  string
	Callers       int
	CandidateText string // the full clause rendered after "Candidate:"
	// CandidateModel is the nominated model id, empty for same model or
	// shape only candidates. The eval generator keys off it.
	CandidateModel string
	Tripwire       string
	TripwireCheck  TripwireCheck
	Flags          []string
}

// Engine evaluates the loaded rules against a scan report. Volumes is
// the measured traffic file when one was given, nil otherwise;
// DefaultVolumeSource names where Est.Volume.CallsPerMonth came from,
// for the sites the file does not cover.
type Engine struct {
	Rules               []Rule
	Est                 Estimates
	Volumes             *Volumes
	DefaultVolumeSource string
}

// Load reads the embedded rule and estimate files.
func Load() (*Engine, error) {
	entries, err := ruleFiles.ReadDir(".")
	if err != nil {
		return nil, err
	}
	e := &Engine{DefaultVolumeSource: VolumeEstimate}
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
	if err := e.Est.validate(); err != nil {
		return nil, fmt.Errorf("estimates.yaml: %w", err)
	}
	return e, nil
}

func (est Estimates) validate() error {
	if est.Volume.CallsPerMonth <= 0 || est.Tokens.CharsPerToken <= 0 {
		return fmt.Errorf("missing volume or token assumptions")
	}
	// A zero here would price every schema bounded call at the envelope
	// alone, which is a bound nobody chose.
	if so := est.Tokens.SchemaOutput; so.Envelope <= 0 || so.PerField <= 0 || so.PerEnumField <= 0 {
		return fmt.Errorf("tokens.schema_output needs a positive envelope, per_field and per_enum_field")
	}
	if f := est.Cache.SteadyStateReadFraction; f <= 0 || f > 1 {
		return fmt.Errorf("cache.steady_state_read_fraction must be in (0, 1]")
	}
	for _, status := range est.FanIn.MultiplyWhen {
		if !knownFanInStatuses[status] {
			return fmt.Errorf("fan_in.multiply_when names unknown status %q", status)
		}
	}
	return nil
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
	if err := r.TripwireCheck.validate(); err != nil {
		return fmt.Errorf("%s: %w", r.ID, err)
	}
	// Enumerated when values are checked against their closed sets; a
	// typo would otherwise load fine and disable the rule. Providers are
	// the exception: the catalog owns that namespace, so a name today's
	// catalog lacks may be valid against tomorrow's.
	for _, tier := range r.When.Tier {
		if !slices.Contains(catalog.Tiers, tier) {
			return fmt.Errorf("%s: unknown tier %q in when.tier", r.ID, tier)
		}
	}
	for _, a := range r.When.Archetype {
		if !knownArchetypes[a] {
			return fmt.Errorf("%s: unknown archetype %q in when.archetype", r.ID, a)
		}
	}
	for _, a := range r.When.ArchetypeNot {
		if !knownArchetypes[a] {
			return fmt.Errorf("%s: unknown archetype %q in when.archetype_not", r.ID, a)
		}
	}
	for _, ef := range r.When.Effort {
		if !knownEfforts[ef] {
			return fmt.Errorf("%s: unknown effort %q in when.effort", r.ID, ef)
		}
	}
	if c := r.When.ModelCapability; c != "" && !slices.Contains(catalog.Capabilities, c) {
		return fmt.Errorf("%s: unknown model_capability %q", r.ID, c)
	}
	if r.Candidate.Strategy == "price_multiplier" && (r.Candidate.Multiplier <= 0 || r.Candidate.Multiplier > 1) {
		return fmt.Errorf("%s: price_multiplier needs a multiplier in (0, 1], got %g", r.ID, r.Candidate.Multiplier)
	}
	return nil
}

// Clone returns an engine one repository's config can retune without
// reaching any other's. A slice copy is enough: the mutators replace
// whole fields, never write through a Rule's pointers. Volumes is
// shared, not copied; nothing writes to it after parsing.
func (e *Engine) Clone() *Engine {
	c := &Engine{
		Est:                 e.Est,
		Rules:               make([]Rule, len(e.Rules)),
		Volumes:             e.Volumes,
		DefaultVolumeSource: e.DefaultVolumeSource,
	}
	copy(c.Rules, e.Rules)
	return c
}

// Disable removes the named rules; unknown ids are a no-op so configs
// survive rule renames. Kept rules go into a fresh slice; filtering in
// place would rewrite a clone's backing array too.
func (e *Engine) Disable(ids []string) {
	drop := map[string]bool{}
	for _, id := range ids {
		drop[id] = true
	}
	kept := make([]Rule, 0, len(e.Rules))
	for _, r := range e.Rules {
		if !drop[r.ID] {
			kept = append(kept, r)
		}
	}
	e.Rules = kept
}

// SetThreshold overrides one numeric When field on the named rule. An
// unknown rule or field is an error.
func (e *Engine) SetThreshold(ruleID, field string, value float64) error {
	for i := range e.Rules {
		if e.Rules[i].ID != ruleID {
			continue
		}
		w := &e.Rules[i].When
		switch field {
		case "min_system_tokens":
			w.MinSystemTokens = int(value)
		case "min_input_per_mtok":
			w.MinInputPerMtok = value
		case "min_retries":
			w.MinRetries = int(value)
		case "min_duplicate_sites":
			w.MinDuplicateSites = int(value)
		case "temperature_above":
			v := value
			w.TemperatureAbove = &v
		default:
			return fmt.Errorf("rule %s has no numeric threshold %q", ruleID, field)
		}
		return nil
	}
	return fmt.Errorf("no rule %q to set a threshold on", ruleID)
}
