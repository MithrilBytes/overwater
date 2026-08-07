package catalog

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
)

// Price drift detection against LiteLLM's community pricing file. The
// file arrives as a local path, never a fetch; the binary's only
// permitted network call stays the catalog itself.

// LitellmEntry is one upstream record, prices in dollars per million.
// HasOutput distinguishes an upstream output price of zero from an
// upstream file that simply omits the field; embedding style entries
// often carry only input_cost_per_token.
type LitellmEntry struct {
	Input       float64
	Output      float64
	HasOutput   bool
	MaxInput    int
	Deprecation string
}

// LitellmPrices maps a model key to its upstream record.
type LitellmPrices map[string]LitellmEntry

// ParseLitellm reads LiteLLM's model_prices_and_context_window.json,
// keeping entries that carry per token costs and skipping the rest.
// Context windows and deprecation dates ride along when present, so
// the nightly diff can report their drift too.
func ParseLitellm(raw []byte) (LitellmPrices, error) {
	var entries map[string]json.RawMessage
	if err := json.Unmarshal(raw, &entries); err != nil {
		return nil, fmt.Errorf("litellm pricing file: %w", err)
	}
	prices := LitellmPrices{}
	for key, rawEntry := range entries {
		var e struct {
			Input       *float64 `json:"input_cost_per_token"`
			Output      *float64 `json:"output_cost_per_token"`
			MaxInput    int      `json:"max_input_tokens"`
			Deprecation string   `json:"deprecation_date"`
		}
		if err := json.Unmarshal(rawEntry, &e); err != nil || e.Input == nil {
			continue
		}
		entry := LitellmEntry{
			Input:       *e.Input * 1e6,
			MaxInput:    e.MaxInput,
			Deprecation: e.Deprecation,
		}
		if e.Output != nil {
			entry.Output = *e.Output * 1e6
			entry.HasOutput = true
		}
		prices[key] = entry
	}
	return prices, nil
}

// Drift is one entry whose price disagrees with LiteLLM. When the
// upstream record omits output_cost_per_token, TheirsOutKnown is false,
// TheirsOut is meaningless, and ApplyPrices leaves our output price
// alone.
type Drift struct {
	ID             string
	OursIn         float64
	OursOut        float64
	TheirsIn       float64
	TheirsOut      float64
	TheirsOutKnown bool
}

// DiffLitellm compares active catalog entries against LiteLLM prices,
// matching by id, alias, and provider prefixed variants of both.
// Deprecated entries keep their historical prices and are skipped.
// Prices come back as applyable drift; context window and deprecation
// disagreements come back as notes for a human, never auto applied.
func DiffLitellm(c *Catalog, prices LitellmPrices) (drifts []Drift, notes, missing []string) {
	for _, m := range c.Models {
		if m.Deprecated != "" {
			continue
		}
		keys := []string{m.ID, m.Provider + "/" + m.ID}
		for _, a := range m.Aliases {
			keys = append(keys, a, m.Provider+"/"+a)
		}
		found := false
		for _, k := range keys {
			p, ok := prices[k]
			if !ok {
				continue
			}
			found = true
			// An upstream record with no output price says nothing
			// about our output price; comparing against its zero value
			// would propose zeroing ours.
			outDrifts := p.HasOutput && differs(m.OutputPerMtok, p.Output)
			if p.Input > 0 && (differs(m.InputPerMtok, p.Input) || outDrifts) {
				drifts = append(drifts, Drift{
					ID: m.ID, OursIn: m.InputPerMtok, OursOut: m.OutputPerMtok,
					TheirsIn: p.Input, TheirsOut: p.Output, TheirsOutKnown: p.HasOutput,
				})
			}
			if p.MaxInput > 0 && p.MaxInput != m.ContextWindow {
				notes = append(notes, fmt.Sprintf("%s: context window ours %d, litellm %d", m.ID, m.ContextWindow, p.MaxInput))
			}
			if p.Deprecation != "" {
				notes = append(notes, fmt.Sprintf("%s: litellm lists deprecation date %s; our entry is active", m.ID, p.Deprecation))
			}
			break
		}
		if !found {
			missing = append(missing, m.ID)
		}
	}
	return drifts, notes, missing
}

// differs allows half a percent of slack so float dust and rounding in
// the upstream file do not read as price changes.
func differs(ours, theirs float64) bool {
	diff := ours - theirs
	if diff < 0 {
		diff = -diff
	}
	base := ours
	if base == 0 {
		return diff > 0
	}
	return diff/base > 0.005
}

var (
	reInputLine  = regexp.MustCompile(`(?m)^input_per_mtok: .*$`)
	reOutputLine = regexp.MustCompile(`(?m)^output_per_mtok: .*$`)
)

// ApplyPrices rewrites the drifted entries in place, bumps VERSION, and
// regenerates catalog.json, leaving everything else in each file
// untouched.
func ApplyPrices(dir string, drifts []Drift, version string) error {
	for _, d := range drifts {
		path := filepath.Join(dir, "models", d.ID+".yaml")
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		out := reInputLine.ReplaceAll(raw, []byte("input_per_mtok: "+formatPrice(d.TheirsIn)))
		if d.TheirsOutKnown {
			out = reOutputLine.ReplaceAll(out, []byte("output_per_mtok: "+formatPrice(d.TheirsOut)))
		}
		if err := os.WriteFile(path, out, 0o644); err != nil {
			return err
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "VERSION"), []byte(version+"\n"), 0o644); err != nil {
		return err
	}
	c, err := LoadDir(dir)
	if err != nil {
		return err
	}
	b, err := c.JSON()
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "catalog.json"), b, 0o644); err != nil {
		return err
	}
	// Dated snapshot, so price drift has a history in the repo.
	if err := os.MkdirAll(filepath.Join(dir, "history"), 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "history", version+".json"), b, 0o644)
}

func formatPrice(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64)
}
