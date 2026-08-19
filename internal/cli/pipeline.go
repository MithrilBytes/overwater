package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/MithrilBytes/overwater/catalog"
	"github.com/MithrilBytes/overwater/internal/render"
	"github.com/MithrilBytes/overwater/internal/scan"
	"github.com/MithrilBytes/overwater/rules"
)

// pipeline is the catalog and rule set of one invocation. base is never
// mutated after load: every root scans its own clone.
type pipeline struct {
	cat  *catalog.Catalog
	base *rules.Engine
	meta render.Meta
	// volumesPath labels the stderr notes about the volumes file; empty
	// when the run has none.
	volumesPath string
}

// rootPlan pairs a root with its .overwater.yaml, nil when it has none.
// Configs load before any scanning, so a malformed one fails the run
// before half a report is printed. only restricts findings to those
// root relative files, nil for all of them.
type rootPlan struct {
	root string
	cfg  *repoConfig
	only map[string]bool
}

// planRoot resolves one argument. A named file becomes its containing
// directory restricted to that file, so imports and wrapper defaults
// resolve as they would in a whole repo scan and the directory's
// .overwater.yaml applies.
func planRoot(root string) (rootPlan, error) {
	info, err := os.Stat(root)
	if err != nil {
		return rootPlan{}, fmt.Errorf("scan %s: %w", root, err)
	}
	pl := rootPlan{root: root}
	if !info.IsDir() {
		pl.root = filepath.Dir(root)
		pl.only = map[string]bool{filepath.Base(root): true}
	}
	cfg, err := loadRepoConfig(pl.root)
	if err != nil {
		return rootPlan{}, err
	}
	pl.cfg = cfg
	return pl, nil
}

// planRoots resolves every argument, merging files that share a
// directory into one plan. A pre commit hook passes a list of files;
// one plan per file would walk the directory once per file and prefix
// every finding with the directory's name.
func planRoots(roots []string) ([]rootPlan, error) {
	plans := make([]rootPlan, 0, len(roots))
	filePlan := map[string]int{}
	for _, r := range roots {
		pl, err := planRoot(r)
		if err != nil {
			return nil, err
		}
		if i, ok := filePlan[pl.root]; ok && pl.only != nil {
			for f := range pl.only {
				plans[i].only[f] = true
			}
			continue
		}
		if pl.only != nil {
			filePlan[pl.root] = len(plans)
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

// volumeFor is one root's fallback calls per month and its provenance.
// An explicit --volume, already folded into the base estimates, beats
// the root's config, which beats the estimate default; a volumes file
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
// only when every root agrees; disagreeing roots fall back to the
// estimate default and are named on stderr.
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
// no network) and the rules. Notes go to stderr; stdout belongs to the
// renderers.
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
// disabled rule or a moved threshold stays inside the repo that set it.
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
// budget_monthly_usd is exceeded; unmatched names the volumes file keys
// no call site in this root used.
type rootResult struct {
	findings   []rules.Finding
	overBudget string
	unmatched  []string
	// unrecognized names model looking strings the catalog does not
	// carry. They are priced at nothing and so produce no findings,
	// which without a word here reads as a clean bill of health.
	unrecognized []string
	// unrecognizedConfig says the same about values traced out of a
	// config file, by the key that held them rather than by the value.
	unrecognizedConfig []string
	// scanned is how many files the walk admitted, so a root the
	// scanner never opened does not read as a root with nothing wrong.
	scanned int
	// sdksMissed are SDKs a manifest declares where the scan resolved no
	// call site at all. Layer 1 knows the repository talks to a model;
	// saying so is the difference between "nothing to fix" and "we could
	// not find what we know is there".
	sdksMissed []scan.SDK
	// unpriced are calls that spend tokens without naming a model at
	// all: an HTTP endpoint whose model is a runtime variable, or an
	// agent CLI invocation. Same contract, same reason.
	unpriced []scan.UnpricedCall
}

// scanRoot scans one root under its own config. A non nil only set
// restricts the scan to those root relative files, except where the
// plan names files of its own: those the user asked for by name win.
func (p *pipeline) scanRoot(pl rootPlan, only map[string]bool, vol volumeChoice) (rootResult, error) {
	eng, err := p.engineFor(pl, vol)
	if err != nil {
		return rootResult{}, err
	}
	if pl.only != nil {
		only = pl.only
	}
	report, err := scan.AnalyzeOnly(pl.root, p.cat, only)
	if err != nil {
		return rootResult{}, err
	}
	res := rootResult{
		findings:           dropExcluded(pl.cfg, eng.Evaluate(report, p.cat)),
		unmatched:          eng.UnmatchedVolumeKeys(report, p.cat),
		unrecognized:       unrecognizedModels(report),
		unrecognizedConfig: unrecognizedConfigKeys(report),
		unpriced:           report.Unpriced,
		scanned:            report.Scanned,
	}
	if len(report.Sites) == 0 {
		res.sdksMissed = report.SDKs
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
// name to stay attributable and report counts on stderr; a single root
// is left alone. Each over budget root contributes one line.
func (p *pipeline) scanPlans(plans []rootPlan, only map[string]bool, vol volumeChoice, stderr io.Writer) ([]rules.Finding, []string, error) {
	multi := len(plans) > 1
	var findings []rules.Finding
	var overBudgets []string
	misses := map[string]int{}
	unknown := map[string]bool{}
	unknownConfig := map[string]bool{}
	var unpriced []scan.UnpricedCall
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
		for _, name := range res.unrecognized {
			unknown[name] = true
		}
		for _, where := range res.unrecognizedConfig {
			unknownConfig[where] = true
		}
		unpriced = append(unpriced, res.unpriced...)
		rf := res.findings
		if multi {
			prefix := filepath.Base(filepath.Clean(pl.root)) + "/"
			for i := range rf {
				rf[i].File = prefix + rf[i].File
			}
			fmt.Fprintf(stderr, "%s: %d findings\n", pl.root, len(rf))
		}
		findings = append(findings, rf...)
		if res.scanned == 0 {
			fmt.Fprintf(stderr, "overwater: %s: no files to scan\n", pl.root)
		}
		reportMissedSDKs(res.sdksMissed, stderr)
	}
	p.reportUnmatched(misses, len(plans), stderr)
	reportUnrecognized(unknown, unknownConfig, p.cat.Version, stderr)
	reportUnpriced(unpriced, stderr)
	return findings, overBudgets, nil
}

// dropExcluded removes findings in files the repo config excludes. The
// only other lever is disable, which is repo wide, so a generated file
// that merely names model ids used to cost the rule everywhere.
func dropExcluded(cfg *repoConfig, findings []rules.Finding) []rules.Finding {
	if cfg == nil || len(cfg.Exclude) == 0 {
		return findings
	}
	kept := findings[:0]
	for _, f := range findings {
		if !cfg.excluded(f.File) {
			kept = append(kept, f)
		}
	}
	return kept
}

// reportMissedSDKs says when a manifest declares an LLM SDK and the scan
// resolved no call site. Layer 1 reads those manifests and had nowhere
// to put the answer, so a repository whose calls the scanner cannot see
// was indistinguishable from one that makes none.
func reportMissedSDKs(sdks []scan.SDK, stderr io.Writer) {
	if len(sdks) == 0 {
		return
	}
	names := map[string]bool{}
	for _, s := range sdks {
		names[s.Name] = true
	}
	fmt.Fprintf(stderr, "overwater: %s declared but no call site found: %s\n",
		plural(len(names), "an SDK is", "SDKs are"), strings.Join(cappedNames(names), ", "))
}

// reportUnpriced names calls that spend tokens with no model to price.
// An HTTP call whose model is a runtime variable, and an agent CLI
// invocation, are both real spend that this scanner cannot cost: a price
// needs a catalog entry, and neither a variable nor "opus" is one.
// Saying where knowledge stops is the honest alternative to a verdict
// that reads as though the repository were clean.
func reportUnpriced(calls []scan.UnpricedCall, stderr io.Writer) {
	if len(calls) == 0 {
		return
	}
	sort.Slice(calls, func(i, j int) bool {
		if calls[i].File != calls[j].File {
			return calls[i].File < calls[j].File
		}
		return calls[i].Line < calls[j].Line
	})
	shown := calls
	if len(shown) > maxUnpricedNamed {
		shown = shown[:maxUnpricedNamed]
	}
	fmt.Fprintf(stderr, "overwater: %d call %s no model to price:\n",
		len(calls), plural(len(calls), "site names", "sites name"))
	for _, c := range shown {
		fmt.Fprintf(stderr, "  %s:%d (%s) %s\n", c.File, c.Line, c.Kind, c.Evidence)
	}
	if len(calls) > len(shown) {
		fmt.Fprintf(stderr, "  and %d more\n", len(calls)-len(shown))
	}
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

const maxUnpricedNamed = 5

// unrecognizedModels lists the distinct model looking strings in a
// report that the catalog does not carry. A string traced out of a
// config file is not one of them; see unrecognizedConfigKeys.
func unrecognizedModels(report *scan.Report) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range report.Sites {
		if s.Known || s.Ref == "" || s.ViaConfig != "" || seen[s.Ref] {
			continue
		}
		seen[s.Ref] = true
		out = append(out, s.Ref)
	}
	sort.Strings(out)
	return out
}

// unrecognizedConfigKeys names where an unresolved value came from,
// as "path KEY", for the sites config tracing built (scan.Site.ViaConfig).
//
// The value itself is never repeated. It was read out of a config file
// by nothing stronger than a key that mentions MODEL or DEPLOYMENT and
// a value holding a digit or a dash, which is also an exact description
// of MODEL_API_KEY, MODEL_ENDPOINT and DEPLOYMENT_TOKEN. This note is
// printed on stderr, and the action puts stderr in the job log, the
// step summary and a pull request comment, none of which mask a value
// that was never registered as an Actions secret. The key and its file
// are enough for the operator to go and look.
func unrecognizedConfigKeys(report *scan.Report) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range report.Sites {
		if s.Known || s.ViaConfig == "" || seen[s.ViaConfig] {
			continue
		}
		seen[s.ViaConfig] = true
		out = append(out, s.ViaConfig)
	}
	sort.Strings(out)
	return out
}

// reportUnrecognized says which model strings were found but could not
// be priced. Silence here is the worst answer available: a repository
// pinning a model the catalog has never heard of otherwise gets the same
// "keep the models you have" as a repository that is genuinely fine.
func reportUnrecognized(names, viaConfig map[string]bool, catalogVersion string, stderr io.Writer) {
	if len(names) > 0 {
		list := cappedNames(names)
		fmt.Fprintf(stderr, "overwater: not in catalog %s, so not priced: %s\n",
			catalogVersion, strings.Join(list, ", "))
		// Upstream usually knows these already, and the reverse diff turns
		// them into entries to add rather than a shrug.
		fmt.Fprintf(stderr, "  look them up: overwater catalog diff -reverse -only %s <litellm.json>\n",
			strings.Join(list, ","))
	}
	if len(viaConfig) > 0 {
		// Where the value sits, not what it says, and so no reverse diff
		// line: there is no id here to look up, only a place to read.
		fmt.Fprintf(stderr, "overwater: config values not in catalog %s, so not priced: %s\n",
			catalogVersion, strings.Join(cappedNames(viaConfig), ", "))
	}
}

// cappedNames sorts a set for printing and folds the tail into a count.
func cappedNames(set map[string]bool) []string {
	var list []string
	for name := range set {
		list = append(list, name)
	}
	sort.Strings(list)
	if len(list) > maxUnrecognizedNamed {
		list = append(list[:maxUnrecognizedNamed:maxUnrecognizedNamed],
			fmt.Sprintf("and %d more", len(set)-maxUnrecognizedNamed))
	}
	return list
}

// A repository can name a lot of models it does not call; the point is
// to be told, not to be given a wall of text.
const maxUnrecognizedNamed = 8

// reportUnmatched names the volumes file keys that matched nothing
// anywhere. A key is unknown only when every root missed it: under
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
