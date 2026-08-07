package cli

import (
	"fmt"
	"io"
	"path/filepath"
	"time"

	"github.com/MithrilBytes/overwater/catalog"
	"github.com/MithrilBytes/overwater/internal/render"
	"github.com/MithrilBytes/overwater/internal/scan"
	"github.com/MithrilBytes/overwater/rules"
)

// pipeline is the loaded catalog and the pristine rule set of one
// invocation. The catalog load is the expensive part and happens once;
// base is never evaluated or mutated after load, because every root
// scans with its own clone.
type pipeline struct {
	cat  *catalog.Catalog
	base *rules.Engine
	meta render.Meta
}

// rootPlan pairs a root with its own .overwater.yaml, nil when the root
// has none. Configs load before any scanning so a malformed one fails
// the run before it prints half a report.
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

// volumeFor is one root's own calls per month: its config's volume, or
// the estimate default. An explicit --volume already sits in the base
// estimates and beats both.
func (p *pipeline) volumeFor(pl rootPlan, flagVolume int) int {
	if flagVolume == 0 && pl.cfg != nil && pl.cfg.Volume > 0 {
		return pl.cfg.Volume
	}
	return p.base.Est.Volume.CallsPerMonth
}

// volumeAcross picks the one volume a merged report is priced and
// headed at. A report carries a single header, so per root volumes are
// honored only when every root resolves to the same number; otherwise
// the header would name a volume the body does not use. Roots that
// disagree fall back to the estimate default and are named on stderr.
func (p *pipeline) volumeAcross(plans []rootPlan, flagVolume int, stderr io.Writer) int {
	def := p.base.Est.Volume.CallsPerMonth
	agreed := def
	for i, pl := range plans {
		v := p.volumeFor(pl, flagVolume)
		if i == 0 {
			agreed = v
			continue
		}
		if v != agreed {
			fmt.Fprintf(stderr, "%s: %s volume %d disagrees with %d elsewhere; pricing every root at %d\n",
				pl.root, configName, v, agreed, def)
			return def
		}
	}
	return agreed
}

// newPipeline picks the effective catalog (embedded or a newer cache,
// zero network) and loads the rules. Advisory notes such as a bad cache
// or stale prices go to stderr; stdout belongs to the renderers.
func newPipeline(volume int, stderr io.Writer) (*pipeline, error) {
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
	return &pipeline{
		cat:  cat,
		base: engine,
		meta: render.Meta{CatalogVersion: cat.Version, CallsPerMonth: engine.Est.Volume.CallsPerMonth},
	}, nil
}

// engineFor builds the engine that judges one root: a fresh clone with
// that root's config folded in and nothing from any other root. The
// clone is what keeps a disabled rule or a moved threshold inside the
// repository that asked for it.
func (p *pipeline) engineFor(pl rootPlan, volume int) (*rules.Engine, error) {
	eng := p.base.Clone()
	eng.Est.Volume.CallsPerMonth = volume
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

// scanRoot runs the scanner and rules over one root under that root's
// own config and nothing else. volume is the calls per month this root
// is priced at, resolved by the caller. A non nil only set restricts
// the scan to those root relative files. overBudget is one line naming
// total and budget when the config's budget_monthly_usd is exceeded,
// empty otherwise.
func (p *pipeline) scanRoot(pl rootPlan, only map[string]bool, volume int) ([]rules.Finding, string, error) {
	eng, err := p.engineFor(pl, volume)
	if err != nil {
		return nil, "", err
	}
	report, err := scan.AnalyzeOnly(pl.root, p.cat, only)
	if err != nil {
		return nil, "", err
	}
	findings := eng.Evaluate(report, p.cat)
	overBudget := ""
	if pl.cfg != nil && pl.cfg.BudgetMonthlyUSD > 0 {
		if total := eng.TotalMonthlyUSD(report, p.cat); total > pl.cfg.BudgetMonthlyUSD {
			overBudget = fmt.Sprintf("estimated ~$%.0f/mo across all known call sites exceeds budget_monthly_usd %g",
				total, pl.cfg.BudgetMonthlyUSD)
		}
	}
	return findings, overBudget, nil
}

// scanPlans scans every planned root at one volume and merges the
// results. With more than one root, findings are prefixed with the
// root's base name so they stay attributable and each root's count goes
// to stderr; a single root keeps today's byte identical output. Each
// over budget root contributes one line.
func (p *pipeline) scanPlans(plans []rootPlan, only map[string]bool, volume int, stderr io.Writer) ([]rules.Finding, []string, error) {
	multi := len(plans) > 1
	var findings []rules.Finding
	var overBudgets []string
	for _, pl := range plans {
		rf, overBudget, err := p.scanRoot(pl, only, volume)
		if err != nil {
			return nil, nil, err
		}
		if overBudget != "" {
			overBudgets = append(overBudgets, overBudget)
		}
		if multi {
			prefix := filepath.Base(filepath.Clean(pl.root)) + "/"
			for i := range rf {
				rf[i].File = prefix + rf[i].File
			}
			fmt.Fprintf(stderr, "%s: %d findings\n", pl.root, len(rf))
		}
		findings = append(findings, rf...)
	}
	return findings, overBudgets, nil
}
