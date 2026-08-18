// Package catalog holds the model catalog: the pricing and capability data
// the scanner prices findings with, and the detection dictionary it scans
// source code against. Entries live as one YAML file per model under
// models/, and build emits the single catalog.json that ships embedded in
// the binary and published to GitHub Pages.
package catalog

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Tiers a model entry may declare, from most to least capable, with
// embedding models in their own lane.
var Tiers = []string{"frontier", "mid", "small", "embedding"}

// Model is one catalog entry. Prices are US dollars per million tokens.
// Deprecated, when set, is the date the provider retires or retired the
// model; empty means active.
type Model struct {
	ID            string   `yaml:"id" json:"id"`
	Provider      string   `yaml:"provider" json:"provider"`
	Aliases       []string `yaml:"aliases,omitempty" json:"aliases,omitempty"`
	InputPerMtok  float64  `yaml:"input_per_mtok" json:"input_per_mtok"`
	OutputPerMtok float64  `yaml:"output_per_mtok" json:"output_per_mtok"`
	// Cache read and write rates let caching findings price exactly;
	// batch_multiplier is the provider's batch endpoint discount.
	// All optional: absent means no data, never free.
	CacheReadPerMtok  float64  `yaml:"cache_read_per_mtok,omitempty" json:"cache_read_per_mtok,omitempty"`
	CacheWritePerMtok float64  `yaml:"cache_write_per_mtok,omitempty" json:"cache_write_per_mtok,omitempty"`
	BatchMultiplier   float64  `yaml:"batch_multiplier,omitempty" json:"batch_multiplier,omitempty"`
	Capabilities      []string `yaml:"capabilities,omitempty" json:"capabilities,omitempty"`
	ContextWindow     int      `yaml:"context_window" json:"context_window"`
	Tier              string   `yaml:"tier" json:"tier"`
	Released          string   `yaml:"released" json:"released"`
	Deprecated        string   `yaml:"deprecated,omitempty" json:"deprecated,omitempty"`
	Source            string   `yaml:"source" json:"source"`
}

// Capabilities a model entry may declare. Rule files validate their
// model_capability predicates against this same list. reasoning is the
// odd one out: it does not say what the call may ask for, it says the
// model bills a hidden chain on the output leg, so an entry carrying it
// cannot be priced off the visible answer alone.
var Capabilities = []string{
	"vision", "tools", "caching", "structured_output", "audio", "dimensions", "reasoning",
}

// HasCapability reports whether the entry declares a capability.
func (m Model) HasCapability(c string) bool {
	return slices.Contains(m.Capabilities, c)
}

// Catalog is the emitted artifact. Version is the date the prices were
// last verified, not a release number. RosterVerified dates the model
// list itself: prices refresh nightly and the roster does not, so one
// date for both lets a catalog missing a year of releases still report
// itself fresh. Empty when the catalog records no roster date.
type Catalog struct {
	Version        string  `json:"version"`
	RosterVerified string  `json:"roster_verified,omitempty"`
	Models         []Model `json:"models"`
}

const dateLayout = "2006-01-02"

func (m Model) validate() error {
	if m.ID == "" {
		return fmt.Errorf("entry is missing an id")
	}
	if m.Provider == "" {
		return fmt.Errorf("%s: provider is required", m.ID)
	}
	if m.InputPerMtok <= 0 {
		return fmt.Errorf("%s: input_per_mtok must be positive", m.ID)
	}
	if m.OutputPerMtok < 0 {
		return fmt.Errorf("%s: output_per_mtok must not be negative", m.ID)
	}
	if m.ContextWindow <= 0 {
		return fmt.Errorf("%s: context_window must be positive", m.ID)
	}
	if !slices.Contains(Tiers, m.Tier) {
		return fmt.Errorf("%s: tier %q is not one of %s", m.ID, m.Tier, strings.Join(Tiers, ", "))
	}
	if _, err := time.Parse(dateLayout, m.Released); err != nil {
		return fmt.Errorf("%s: released %q is not a YYYY-MM-DD date", m.ID, m.Released)
	}
	if m.Deprecated != "" {
		if _, err := time.Parse(dateLayout, m.Deprecated); err != nil {
			return fmt.Errorf("%s: deprecated %q is not a YYYY-MM-DD date", m.ID, m.Deprecated)
		}
	}
	if !strings.HasPrefix(m.Source, "https://") {
		return fmt.Errorf("%s: source must be an https URL backing the pricing claim", m.ID)
	}
	if m.CacheReadPerMtok < 0 || m.CacheWritePerMtok < 0 {
		return fmt.Errorf("%s: cache rates must not be negative", m.ID)
	}
	if m.BatchMultiplier < 0 || m.BatchMultiplier > 1 {
		return fmt.Errorf("%s: batch_multiplier must be between 0 and 1", m.ID)
	}
	for _, c := range m.Capabilities {
		if !slices.Contains(Capabilities, c) {
			return fmt.Errorf("%s: unknown capability %q", m.ID, c)
		}
	}
	return nil
}

// Validate checks every entry and the catalog level invariants: a dated
// version, a roster date that parses when one is carried, at least one
// model, and globally unique names across ids and aliases. An empty
// model list is an empty detection dictionary; a cache carrying one
// would blind every scan.
func (c *Catalog) Validate() error {
	if _, err := time.Parse(dateLayout, c.Version); err != nil {
		return fmt.Errorf("catalog version %q is not a YYYY-MM-DD date", c.Version)
	}
	if c.RosterVerified != "" {
		if _, err := time.Parse(dateLayout, c.RosterVerified); err != nil {
			return fmt.Errorf("catalog roster_verified %q is not a YYYY-MM-DD date", c.RosterVerified)
		}
	}
	if len(c.Models) == 0 {
		return fmt.Errorf("catalog %s has no models", c.Version)
	}
	owner := map[string]string{}
	for _, m := range c.Models {
		if err := m.validate(); err != nil {
			return err
		}
		for _, name := range append([]string{m.ID}, m.Aliases...) {
			if prev, taken := owner[name]; taken {
				return fmt.Errorf("name %q appears in both %s and %s", name, prev, m.ID)
			}
			owner[name] = m.ID
		}
	}
	return nil
}

// LoadDir reads a catalog source directory: models/*.yaml entries plus a
// VERSION file holding the price date and an optional ROSTER file holding
// the date the model list was last reconciled with the providers. Entries
// are validated and sorted by id so the emitted JSON is deterministic.
func LoadDir(dir string) (*Catalog, error) {
	rawVersion, err := os.ReadFile(filepath.Join(dir, "VERSION"))
	if err != nil {
		return nil, fmt.Errorf("read catalog version: %w", err)
	}
	// ROSTER is optional: a catalog dir predating it still prices, it
	// just cannot say how old its model list is.
	rawRoster, err := os.ReadFile(filepath.Join(dir, "ROSTER"))
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("read catalog roster date: %w", err)
	}
	paths, err := filepath.Glob(filepath.Join(dir, "models", "*.yaml"))
	if err != nil {
		return nil, err
	}
	if len(paths) == 0 {
		return nil, fmt.Errorf("no model entries under %s", filepath.Join(dir, "models"))
	}
	var models []Model
	for _, path := range paths {
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		var m Model
		dec := yaml.NewDecoder(bytes.NewReader(raw))
		dec.KnownFields(true)
		if err := dec.Decode(&m); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		if want := m.ID + ".yaml"; filepath.Base(path) != want {
			return nil, fmt.Errorf("%s: file name must match its id (%s)", path, want)
		}
		models = append(models, m)
	}
	sort.Slice(models, func(i, j int) bool { return models[i].ID < models[j].ID })
	c := &Catalog{
		Version:        strings.TrimSpace(string(rawVersion)),
		RosterVerified: strings.TrimSpace(string(rawRoster)),
		Models:         models,
	}
	if err := c.Validate(); err != nil {
		return nil, err
	}
	return c, nil
}

// JSON renders the catalog deterministically: two space indent and a
// trailing newline. LoadDir has already sorted the models.
func (c *Catalog) JSON() ([]byte, error) {
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

// Names maps every id and alias to its entry, forming the detection
// dictionary the scanner matches source code against.
func (c *Catalog) Names() map[string]*Model {
	names := make(map[string]*Model)
	for i := range c.Models {
		m := &c.Models[i]
		names[m.ID] = m
		for _, a := range m.Aliases {
			names[a] = m
		}
	}
	return names
}

// ByName resolves an id or alias to its entry, or nil when unknown.
func (c *Catalog) ByName(name string) *Model {
	return c.Names()[name]
}
