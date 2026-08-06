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

// LitellmPrices maps a model key to dollars per million tokens.
type LitellmPrices map[string]struct {
	Input  float64
	Output float64
}

// ParseLitellm reads LiteLLM's model_prices_and_context_window.json,
// keeping entries that carry per token costs and skipping the rest.
func ParseLitellm(raw []byte) (LitellmPrices, error) {
	var entries map[string]json.RawMessage
	if err := json.Unmarshal(raw, &entries); err != nil {
		return nil, fmt.Errorf("litellm pricing file: %w", err)
	}
	prices := LitellmPrices{}
	for key, rawEntry := range entries {
		var e struct {
			Input  *float64 `json:"input_cost_per_token"`
			Output *float64 `json:"output_cost_per_token"`
		}
		if err := json.Unmarshal(rawEntry, &e); err != nil || e.Input == nil {
			continue
		}
		out := 0.0
		if e.Output != nil {
			out = *e.Output
		}
		prices[key] = struct {
			Input  float64
			Output float64
		}{*e.Input * 1e6, out * 1e6}
	}
	return prices, nil
}

// Drift is one entry whose price disagrees with LiteLLM.
type Drift struct {
	ID        string
	OursIn    float64
	OursOut   float64
	TheirsIn  float64
	TheirsOut float64
}

// DiffLitellm compares active catalog entries against LiteLLM prices,
// matching by id, alias, and provider prefixed variants of both.
// Deprecated entries keep their historical prices and are skipped.
func DiffLitellm(c *Catalog, prices LitellmPrices) (drifts []Drift, missing []string) {
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
			if p.Input > 0 && (differs(m.InputPerMtok, p.Input) || differs(m.OutputPerMtok, p.Output)) {
				drifts = append(drifts, Drift{
					ID: m.ID, OursIn: m.InputPerMtok, OursOut: m.OutputPerMtok,
					TheirsIn: p.Input, TheirsOut: p.Output,
				})
			}
			break
		}
		if !found {
			missing = append(missing, m.ID)
		}
	}
	return drifts, missing
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
		out = reOutputLine.ReplaceAll(out, []byte("output_per_mtok: "+formatPrice(d.TheirsOut)))
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
	return os.WriteFile(filepath.Join(dir, "catalog.json"), b, 0o644)
}

func formatPrice(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64)
}
