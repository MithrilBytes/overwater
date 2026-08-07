package catalog

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// Price drift detection against LiteLLM's community pricing file. The
// file arrives as a local path, never a fetch: the catalog stays the
// binary's only permitted network call.

// LitellmEntry is one upstream record, prices in dollars per million.
// HasOutput separates an upstream output price of zero from an upstream
// file that omits the field, as embedding entries usually do.
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
// Context windows and deprecation dates ride along when present.
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

// Drift is one entry whose price disagrees with LiteLLM. With
// TheirsOutKnown false, TheirsOut is meaningless and ApplyPrices leaves
// our output price alone.
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
			if floatingAlias(a) {
				continue
			}
			keys = append(keys, a, m.Provider+"/"+a)
		}
		found := false
		for _, k := range keys {
			p, ok := prices[k]
			if !ok {
				continue
			}
			found = true
			// Comparing against an absent output price would propose
			// zeroing ours.
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

// floatingAlias reports whether a name points at whatever generation is
// current rather than at one model. Upstream repoints these on release,
// so matching a pinned entry against one reads the next generation's
// price as our drift: mistral-medium-latest moved to medium-3.5 at
// 1.50, which would have proposed a 3.75x rise for medium-3.
func floatingAlias(name string) bool {
	return strings.HasSuffix(name, "-latest")
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
// regenerates catalog.json, leaving the rest of each file untouched. A
// drifted entry whose price line the regex cannot find is an error:
// skipping it would still bump VERSION as if the price had landed.
func ApplyPrices(dir string, drifts []Drift, version string) error {
	for _, d := range drifts {
		path := filepath.Join(dir, "models", d.ID+".yaml")
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		out, n := replaceCounting(raw, reInputLine, "input_per_mtok: "+formatPrice(d.TheirsIn))
		if n == 0 {
			return fmt.Errorf("%s: no input_per_mtok line matched; price not applied", path)
		}
		if d.TheirsOutKnown {
			out, n = replaceCounting(out, reOutputLine, "output_per_mtok: "+formatPrice(d.TheirsOut))
			if n == 0 {
				return fmt.Errorf("%s: no output_per_mtok line matched; price not applied", path)
			}
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

// replaceCounting swaps every match of re for repl and reports how many
// it touched.
func replaceCounting(raw []byte, re *regexp.Regexp, repl string) ([]byte, int) {
	n := 0
	out := re.ReplaceAllFunc(raw, func([]byte) []byte {
		n++
		return []byte(repl)
	})
	return out, n
}

func formatPrice(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64)
}
