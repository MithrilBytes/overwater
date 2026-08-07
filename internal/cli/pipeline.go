package cli

import (
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"time"

	"github.com/MithrilBytes/overwater/catalog"
	"github.com/MithrilBytes/overwater/internal/render"
	"github.com/MithrilBytes/overwater/internal/scan"
	"github.com/MithrilBytes/overwater/rules"
)

// pipeline is the catalog and rule set of one invocation. base is never
// mutated after load: every root scans with its own clone.
type pipeline struct {
	cat  *catalog.Catalog
	base *rules.Engine
	meta render.Meta
	// volumesPath labels the stderr notes about the volumes file; empty
	// when the run has none.
	volumesPath string
}

// rootPlan pairs a root with its own .overwater.yaml, nil when it has
// none. Configs load before any scanning, so a malformed one fails the
// run before half a report is printed.
type rootPlan struct {
	root string
	cfg  *repoConfig
}

func planRoot(root string) (rootPlan, error) {
	cfg, err := loadRepoConfig(root)
	if err != nil {
		return rootPlan{}, err
	}
	return rootPlan{root: root, cfg: cfg}, nil
}

func planRoots(roots []string) ([]rootPlan, error) {
	plans := make([]rootPlan, 0, len(roots))
	for _, r := range roots {
		pl, err := planRoot(r)
		if err != nil {
			return nil, err
		}
		plans = append(plans, pl)
	}
	return plans, nil
}

// volumeChoice is the fallback volume for the call sites no volumes
// file covers, and where that number came from.
type volumeChoice struct {
	calls  int
	source string
}

// volumeFor is one root's fallback calls per month and its provenance:
// an explicit --volume, already folded into the base estimates, beats
// the root's config, which beats the estimate default. A volumes file
// overrides all three per call site.
func (p *pipeline) volumeFor(pl rootPlan, flagVolume int) volumeChoice {
	if flagVolume > 0 {
		return volumeChoice{p.base.Est.Volume.CallsPerMonth, rules.VolumeFlag}
	}
	if pl.cfg != nil && pl.cfg.Volume > 0 {
		return volumeChoice{pl.cfg.Volume, rules.VolumeConfig}
	}
	return volumeChoice{p.base.Est.Volume.CallsPerMonth, rules.VolumeEstimate}
}

// volumeAcross picks the one volume a merged report is priced and
// headed at. A report carries a single header, so per root volumes hold
// only when every root resolves to the same number; disagreeing roots
// fall back to the estimate default and are named on stderr.
func (p *pipeline) volumeAcross(plans []rootPlan, flagVolume int, stderr io.Writer) volumeChoice {
	def := volumeChoice{p.base.Est.Volume.CallsPerMonth, rules.VolumeEstimate}
	agreed := def
	for i, pl := range plans {
		v := p.volumeFor(pl, flagVolume)
		if i == 0 {
			agreed = v
			continue
		}
		if v.calls != agreed.calls {
			fmt.Fprintf(stderr, "%s: %s volume %d disagrees with %d elsewhere; pricing every root at %d\n",
				pl.root, configName, v.calls, agreed.calls, def.calls)
			return def
		}
	}
	return agreed
}

// newPipeline loads the effective catalog (embedded or a newer cache,
// no network) and the rules. Notes such as a bad cache or stale prices
// go to stderr; stdout belongs to the renderers.
func newPipeline(volume int, vols *volumesFile, stderr io.Writer) (*pipeline, error) {
	cat, note, err := catalog.Effective()
	if err != nil {
		return nil, err
	}
	if note != "" {
		fmt.Fprintln(stderr, note)
	}
	if warn := catalog.Stale(cat, time.Now()); warn != "" {
		fmt.Fprintln(stderr, warn)
	}
	engine, err := rules.Load()
	if err != nil {
		return nil, err
	}
	if volume > 0 {
		engine.Est.Volume.CallsPerMonth = volume
	}
	p := &pipeline{
		cat:  cat,
		base: engine,
		meta: render.Meta{CatalogVersion: cat.Version, CallsPerMonth: engine.Est.Volume.CallsPerMonth},
	}
	if vols != nil {
		engine.Volumes = vols.volumes
		p.volumesPath = vols.path
	}
	return p, nil
}

// engineFor clones the base engine and folds in one root's config, so a
// disabled rule or a moved threshold stays inside the repo that asked
// for it.
func (p *pipeline) engineFor(pl rootPlan, vol volumeChoice) (*rules.Engine, error) {
	eng := p.base.Clone()
	eng.Est.Volume.CallsPerMonth = vol.calls
	eng.DefaultVolumeSource = vol.source
	if pl.cfg == nil {
		return eng, nil
	}
	eng.Disable(pl.cfg.Disable)
	for ruleID, fields := range pl.cfg.Thresholds {
		for field, value := range fields {
			if err := eng.SetThreshold(ruleID, field, value); err != nil {
				return nil, fmt.Errorf("%s: %v", configName, err)
			}
		}
	}
	return eng, nil
}

// rootResult is one root's contribution to the merged report.
// overBudget is one line naming total and budget when the config's
// budget_monthly_usd is exceeded, empty otherwise; unmatched names the
// volumes file keys no call site in this root used.
type rootResult struct {
	findings   []rules.Finding
	overBudget string
	unmatched  []string
}

// scanRoot scans one root under its own config and nothing else. A non
// nil only set restricts the scan to those root relative files.
func (p *pipeline) scanRoot(pl rootPlan, only map[string]bool, vol volumeChoice) (rootResult, error) {
	eng, err := p.engineFor(pl, vol)
	if err != nil {
		return rootResult{}, err
	}
	report, err := scan.AnalyzeOnly(pl.root, p.cat, only)
	if err != nil {
		return rootResult{}, err
	}
	res := rootResult{
		findings:  eng.Evaluate(report, p.cat),
		unmatched: eng.UnmatchedVolumeKeys(report, p.cat),
	}
	if pl.cfg != nil && pl.cfg.BudgetMonthlyUSD > 0 {
		if total := eng.TotalMonthlyUSD(report, p.cat); total > pl.cfg.BudgetMonthlyUSD {
			res.overBudget = fmt.Sprintf("~$%.0f/mo across all known call sites exceeds budget_monthly_usd %g",
				total, pl.cfg.BudgetMonthlyUSD)
		}
	}
	return res, nil
}

// scanPlans scans every planned root at one volume and merges the
// results. Several roots prefix their findings with the root's base
// name to stay attributable and report their counts on stderr; a single
// root is left alone. Each over budget root contributes one line.
func (p *pipeline) scanPlans(plans []rootPlan, only map[string]bool, vol volumeChoice, stderr io.Writer) ([]rules.Finding, []string, error) {
	multi := len(plans) > 1
	var findings []rules.Finding
	var overBudgets []string
	misses := map[string]int{}
	for _, pl := range plans {
		res, err := p.scanRoot(pl, only, vol)
		if err != nil {
			return nil, nil, err
		}
		if res.overBudget != "" {
			overBudgets = append(overBudgets, res.overBudget)
		}
		for _, key := range res.unmatched {
			misses[key]++
		}
		rf := res.findings
		if multi {
			prefix := filepath.Base(filepath.Clean(pl.root)) + "/"
			for i := range rf {
				rf[i].File = prefix + rf[i].File
			}
			fmt.Fprintf(stderr, "%s: %d findings\n", pl.root, len(rf))
		}
		findings = append(findings, rf...)
	}
	p.reportUnmatched(misses, len(plans), stderr)
	return findings, overBudgets, nil
}

// reportUnmatched names the volumes file keys that matched nothing
// anywhere. A key is only unknown when every root missed it: under
// several roots each key belongs to one of them.
func (p *pipeline) reportUnmatched(misses map[string]int, roots int, stderr io.Writer) {
	var lines []string
	for key, n := range misses {
		if n == roots {
			lines = append(lines, key)
		}
	}
	sort.Strings(lines)
	for _, line := range lines {
		fmt.Fprintf(stderr, "%s: %s\n", p.volumesPath, line)
	}
}
