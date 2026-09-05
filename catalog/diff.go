package catalog

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Price drift detection against LiteLLM's community pricing file. The
// file arrives as a local path, never a fetch: the catalog stays the
// binary's only permitted network call.

// LitellmEntry is one upstream record, prices in dollars per million.
// HasOutput separates an upstream output price of zero from an upstream
// file that omits the field, as embedding entries usually do.
type LitellmEntry struct {
	Input     float64
	Output    float64
	HasOutput bool
	MaxInput  int
	// Mode and Provider are unused by the price diff and carried for
	// the reverse one, which has to tell a chat model from an image
	// generator and say who serves it.
	Mode        string
	Provider    string
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
			Mode        string   `json:"mode"`
			Provider    string   `json:"litellm_provider"`
			Deprecation string   `json:"deprecation_date"`
		}
		if err := json.Unmarshal(rawEntry, &e); err != nil || e.Input == nil {
			continue
		}
		entry := LitellmEntry{
			Input:       *e.Input * 1e6,
			MaxInput:    e.MaxInput,
			Mode:        e.Mode,
			Provider:    e.Provider,
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

// Deprecation is an upstream retirement date for an entry that has
// none. It is applied like a price: the date is a fact about the
// provider, and an entry without it keeps being nominated as a
// replacement after the model has shut off.
type Deprecation struct {
	ID   string
	Date string
}

// Diff is what one comparison against upstream found, sorted by what
// may be done about it.
type Diff struct {
	// Drifts are prices that moved and may be applied as they stand.
	Drifts []Drift
	// Repointed are prices that moved together with the context window,
	// which is upstream reusing an id for a new generation. Never
	// applied: mistral-medium-3 went to 1.5/7.5 over 262144 while the
	// model this catalog describes stayed at 0.4/2 over 131072 under its
	// dated id, and taking the number would have priced every call site
	// 3.75x high.
	Repointed []Drift
	// Held are prices that moved further than the caller allows in one
	// step. A real cut can be that large, as grok-4's 6x on output was,
	// so these are for a person rather than the nightly job.
	Held []Drift
	// Deprecations are retirement dates upstream lists for entries that
	// carry none, where the window still matches ours.
	Deprecations []Deprecation
	Notes        []string
	// Missing are active entries upstream does not price at all.
	Missing []string
}

// windowTolerance is how far the context windows may disagree before
// the disagreement means a different model. 128000 against 131072 is
// the same window written two ways; 131072 against 262144 is a new
// generation.
const windowTolerance = 1.25

// DiffOptions tune one comparison.
type DiffOptions struct {
	// MaxMove is the largest factor a price may move in one step and
	// still be returned as applyable drift; a larger move is Held. Zero
	// means no limit.
	MaxMove float64
	// Today decides which entries are retired. Zero means now.
	Today time.Time
}

// DiffLitellm compares live catalog entries against LiteLLM prices,
// matching by id, alias, and provider prefixed variants of both. An
// entry the provider has already retired keeps its historical price and
// is skipped; one with a retirement date still ahead of it is live and
// keeps syncing until then.
func DiffLitellm(c *Catalog, prices LitellmPrices, opt DiffOptions) Diff {
	var d Diff
	maxMove := opt.MaxMove
	for _, m := range c.Models {
		if m.Retired(opt.Today) {
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
			windowMoved := p.MaxInput > 0 && ratio(float64(p.MaxInput), float64(m.ContextWindow)) > windowTolerance
			// Comparing against an absent output price would propose
			// zeroing ours.
			outDrifts := p.HasOutput && differs(m.OutputPerMtok, p.Output)
			if p.Input > 0 && (differs(m.InputPerMtok, p.Input) || outDrifts) {
				drift := Drift{
					ID: m.ID, OursIn: m.InputPerMtok, OursOut: m.OutputPerMtok,
					TheirsIn: p.Input, TheirsOut: p.Output, TheirsOutKnown: p.HasOutput,
				}
				switch {
				case windowMoved:
					d.Repointed = append(d.Repointed, drift)
				case maxMove > 0 && moveFactor(drift) > maxMove:
					d.Held = append(d.Held, drift)
				default:
					d.Drifts = append(d.Drifts, drift)
				}
			}
			if windowMoved {
				d.Notes = append(d.Notes, fmt.Sprintf("%s: context window ours %d, litellm %d", m.ID, m.ContextWindow, p.MaxInput))
			}
			if p.Deprecation != "" {
				if windowMoved {
					d.Notes = append(d.Notes, fmt.Sprintf("%s: litellm lists deprecation date %s, but the window moved too", m.ID, p.Deprecation))
				} else {
					d.Deprecations = append(d.Deprecations, Deprecation{ID: m.ID, Date: p.Deprecation})
				}
			}
			break
		}
		if !found {
			d.Missing = append(d.Missing, m.ID)
		}
	}
	return d
}

// ratio is the larger of a/b and b/a, so a move reads the same size in
// either direction.
func ratio(a, b float64) float64 {
	if a <= 0 || b <= 0 {
		return 1
	}
	if a > b {
		return a / b
	}
	return b / a
}

// moveFactor is how far a drift moves the price, on whichever side
// moved more.
func moveFactor(d Drift) float64 {
	f := ratio(d.TheirsIn, d.OursIn)
	if d.TheirsOutKnown {
		if o := ratio(d.TheirsOut, d.OursOut); o > f {
			f = o
		}
	}
	return f
}

// floatingAlias reports whether a name points at whatever generation is
// current rather than at one model. Upstream repoints these on release,
// so a pinned entry matched against one reads the successor's price as
// our drift: mistral-medium-latest moved to medium-3.5 at 1.50, a
// proposed 3.75x rise for medium-3.
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
	reInputLine      = regexp.MustCompile(`(?m)^input_per_mtok: .*$`)
	reOutputLine     = regexp.MustCompile(`(?m)^output_per_mtok: .*$`)
	reCacheReadLine  = regexp.MustCompile(`(?m)^cache_read_per_mtok: *([0-9.]+).*$`)
	reCacheWriteLine = regexp.MustCompile(`(?m)^cache_write_per_mtok: *([0-9.]+).*$`)
)

// scaleCacheRates moves an entry's cache rates by the same factor as its
// input price. Providers publish them as multiples of base input, so a
// rate left at the old price is wrong the moment input moves.
func scaleCacheRates(src string, oldIn, newIn float64) string {
	if oldIn <= 0 || newIn <= 0 || oldIn == newIn {
		return src
	}
	factor := newIn / oldIn
	scale := func(s string, re *regexp.Regexp, key string) string {
		return re.ReplaceAllStringFunc(s, func(line string) string {
			m := re.FindStringSubmatch(line)
			v, err := strconv.ParseFloat(m[1], 64)
			if err != nil {
				return line
			}
			// Rounded: 0.3 times two thirds is 0.19999999999999998,
			// and a price file should not carry float dust.
			return key + ": " + formatPrice(math.Round(v*factor*1e6)/1e6)
		})
	}
	src = scale(src, reCacheReadLine, "cache_read_per_mtok")
	return scale(src, reCacheWriteLine, "cache_write_per_mtok")
}

var reDeprecatedLine = regexp.MustCompile(`(?m)^deprecated:.*\n`)
var reReleasedLine = regexp.MustCompile(`(?m)^(released:.*\n)`)

// ApplyDeprecations writes an upstream retirement date into each entry,
// after its released line so the file reads in order. The date is
// quoted the way the hand written entries quote theirs.
func ApplyDeprecations(dir string, deps []Deprecation) error {
	for _, dep := range deps {
		path := filepath.Join(dir, "models", dep.ID+".yaml")
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		line := fmt.Sprintf("deprecated: %q\n", dep.Date)
		src := string(raw)
		switch {
		case reDeprecatedLine.MatchString(src):
			src = reDeprecatedLine.ReplaceAllString(src, line)
		case reReleasedLine.MatchString(src):
			src = reReleasedLine.ReplaceAllString(src, "${1}"+line)
		default:
			if !strings.HasSuffix(src, "\n") {
				src += "\n"
			}
			src += line
		}
		if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
			return err
		}
	}
	return nil
}

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
		out = []byte(scaleCacheRates(string(out), d.OursIn, d.TheirsIn))
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
