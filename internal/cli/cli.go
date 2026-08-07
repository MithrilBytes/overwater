// Package cli routes overwater's subcommands and owns the exit code
// contract. CI scripts branch on these codes, so findings (1) and
// operational errors (2) are never conflated.
package cli

import (
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/MithrilBytes/overwater/catalog"
	"github.com/MithrilBytes/overwater/internal/baseline"
	"github.com/MithrilBytes/overwater/internal/evalgen"
	"github.com/MithrilBytes/overwater/internal/render"
	"github.com/MithrilBytes/overwater/internal/scan"
	"github.com/MithrilBytes/overwater/rules"
)

// httpClient is the only way any command reaches the network, and only
// the catalog fetch ever uses it. Tests swap the transport to prove
// exactly that.
var httpClient = &http.Client{Timeout: 15 * time.Second}

const (
	// ExitClean means the run finished and nothing violates the failure policy.
	ExitClean = 0
	// ExitFindings means the run finished with findings that violate the
	// failure policy.
	ExitFindings = 1
	// ExitError means the run itself failed: bad invocation, unreadable
	// repo, invalid baseline, malformed catalog.
	ExitError = 2
)

// buildVersion is stamped by the release workflow via ldflags; source
// builds say dev.
var buildVersion = "dev"

// A command is one subcommand of the overwater binary.
type command struct {
	name    string
	summary string
	run     func(args []string, stdout, stderr io.Writer) int
}

// commands is the router table. Order here is the order printed in usage.
var commands = []command{
	{"scan", "report overwatered LLM call sites in a repository", runScan},
	{"diff", "compare two scan --json reports", runDiff},
	{"fleet", "scan every repository in a list file", runFleet},
	{"eval", "generate A/B eval scripts for scan findings", runEval},
	{"catalog", "show or refresh the model catalog", runCatalog},
	{"version", "print the overwater version", runVersion},
}

func runVersion(_ []string, stdout, _ io.Writer) int {
	fmt.Fprintf(stdout, "overwater %s\n", buildVersion)
	return ExitClean
}

// Run executes the subcommand named in args and returns the process exit code.
func Run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		printUsage(stderr)
		return ExitError
	}
	name := args[0]
	if name == "help" || name == "-h" || name == "--help" {
		printUsage(stdout)
		return ExitClean
	}
	for _, cmd := range commands {
		if cmd.name == name {
			return cmd.run(args[1:], stdout, stderr)
		}
	}
	fmt.Fprintf(stderr, "overwater: unknown command %q\n", name)
	printUsage(stderr)
	return ExitError
}

func printUsage(w io.Writer) {
	fmt.Fprint(w, "overwater flags LLM call sites that use more model than the task needs.\n\n")
	fmt.Fprint(w, "Usage:\n\n  overwater <command> [flags]\n\nCommands:\n\n")
	for _, cmd := range commands {
		fmt.Fprintf(w, "  %-9s%s\n", cmd.name, cmd.summary)
	}
	fmt.Fprint(w, "\n")
}

// runScan is the advisor: it prints findings and exits 0 whenever the
// scan itself succeeds. The failure policy that turns findings into a
// nonzero exit arrives with the baseline ratchet.
func runScan(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("scan", flag.ContinueOnError)
	fs.SetOutput(stderr)
	jsonOut := fs.Bool("json", false, "emit findings as JSON instead of text")
	sarifPath := fs.String("sarif", "", "write findings as SARIF 2.1.0 to this path")
	modelsMD := fs.Bool("models-md", false, "write MODELS.md into the scanned repo")
	volume := fs.Int("volume", 0, "estimated calls per month per call site")
	baselinePath := fs.String("baseline", "", "baseline file for the ratchet")
	updateBaseline := fs.Bool("update-baseline", false, "record this scan's findings as the baseline")
	maxAge := fs.Int("max-baseline-age-days", 0, "nag when a matched baseline entry is older than this many days (0 disables)")
	incremental := fs.Bool("incremental", false, "scan only files changed since the baseline's recorded commit")
	failOn := fs.String("fail-on", "new", "failure policy: new, any, or none (none never fails, even over budget)")
	refresh := fs.Bool("refresh", false, "fetch the published catalog before scanning")
	offline := fs.Bool("offline", false, "forbid all network activity")
	htmlOut := fs.String("html", "", "write a single file HTML report to this path")
	csvOut := fs.String("csv", "", "write findings as CSV to this path")
	summary := fs.Bool("summary", false, "print a one line summary instead of the full verdict")
	if err := fs.Parse(args); err != nil {
		return ExitError
	}
	failOnSet := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "fail-on" {
			failOnSet = true
		}
	})
	if *failOn != "new" && *failOn != "any" && *failOn != "none" {
		fmt.Fprintf(stderr, "overwater: unknown --fail-on %q, want new, any, or none\n", *failOn)
		return ExitError
	}
	roots := fs.Args()
	if len(roots) == 0 {
		roots = []string{"."}
	}
	multi := len(roots) > 1
	if multi && *modelsMD {
		fmt.Fprintln(stderr, "overwater: --models-md needs a single root")
		return ExitError
	}
	if *refresh {
		if *offline {
			fmt.Fprintln(stderr, "offline: skipping catalog refresh")
		} else if fetched, raw, err := catalog.Fetch(httpClient, catalog.DefaultURL); err != nil {
			fmt.Fprintf(stderr, "catalog refresh failed: %v; scanning with local prices\n", err)
		} else if _, err := catalog.WriteCache(raw); err != nil {
			fmt.Fprintf(stderr, "could not cache catalog %s: %v\n", fetched.Version, err)
		}
	}
	var only map[string]bool
	if *incremental {
		if multi {
			fmt.Fprintln(stderr, "incremental: multiple roots; scanning everything")
		} else {
			only = incrementalSet(roots[0], defaultBaselinePath(*baselinePath), stderr)
		}
	}
	p, err := newPipeline(*volume, stderr)
	if err != nil {
		fmt.Fprintf(stderr, "overwater: %v\n", err)
		return ExitError
	}
	plans, err := planRoots(roots)
	if err != nil {
		fmt.Fprintf(stderr, "overwater: %v\n", err)
		return ExitError
	}
	// One report, one volume: resolved across every root before the first
	// is scanned, so the header can never name a volume the body did not
	// use.
	callsPerMonth := p.volumeAcross(plans, *volume, stderr)
	p.meta.CallsPerMonth = callsPerMonth
	var findings []rules.Finding
	var overBudgets []string
	for _, pl := range plans {
		rf, overBudget, err := p.scanRoot(pl, only, callsPerMonth)
		if err != nil {
			fmt.Fprintf(stderr, "overwater: %v\n", err)
			return ExitError
		}
		if overBudget != "" {
			overBudgets = append(overBudgets, overBudget)
		}
		if multi {
			// Prefix with the root's base name so merged findings stay
			// attributable; a single root keeps today's byte identical
			// output.
			prefix := filepath.Base(filepath.Clean(pl.root)) + "/"
			for i := range rf {
				rf[i].File = prefix + rf[i].File
			}
			fmt.Fprintf(stderr, "%s: %d findings\n", pl.root, len(rf))
		}
		findings = append(findings, rf...)
	}
	if only != nil {
		// Say how much the restricted scan actually covered, so a null
		// verdict over zero files cannot read as a clean bill of health.
		// Changed files that no longer exist were not scanned; stdout
		// stays untouched.
		scannedFiles := 0
		for f := range only {
			if info, err := os.Stat(filepath.Join(roots[0], filepath.FromSlash(f))); err == nil && info.Mode().IsRegular() {
				scannedFiles++
			}
		}
		fmt.Fprintf(stderr, "incremental: scanned %d of %d candidate files\n", scannedFiles, len(only))
	}
	meta := p.meta
	switch {
	case *jsonOut:
		out, err := render.JSON(findings, meta)
		if err != nil {
			fmt.Fprintf(stderr, "overwater: %v\n", err)
			return ExitError
		}
		stdout.Write(out)
	case *summary:
		fmt.Fprintln(stdout, render.SummaryLine(findings, meta))
	default:
		render.Terminal(stdout, findings, meta)
	}
	if *htmlOut != "" {
		if err := os.WriteFile(*htmlOut, render.HTML(findings, meta), 0o644); err != nil {
			fmt.Fprintf(stderr, "overwater: %v\n", err)
			return ExitError
		}
		fmt.Fprintf(stderr, "wrote %s\n", *htmlOut)
	}
	if *csvOut != "" {
		if err := os.WriteFile(*csvOut, render.CSV(findings), 0o644); err != nil {
			fmt.Fprintf(stderr, "overwater: %v\n", err)
			return ExitError
		}
		fmt.Fprintf(stderr, "wrote %s\n", *csvOut)
	}
	if *sarifPath != "" {
		out, err := render.SARIF(findings, meta)
		if err != nil {
			fmt.Fprintf(stderr, "overwater: %v\n", err)
			return ExitError
		}
		if err := os.WriteFile(*sarifPath, out, 0o644); err != nil {
			fmt.Fprintf(stderr, "overwater: %v\n", err)
			return ExitError
		}
		fmt.Fprintf(stderr, "wrote %s\n", *sarifPath)
	}
	if *modelsMD {
		path := filepath.Join(roots[0], "MODELS.md")
		if err := os.WriteFile(path, render.ModelsMD(findings, meta), 0o644); err != nil {
			fmt.Fprintf(stderr, "overwater: %v\n", err)
			return ExitError
		}
		fmt.Fprintf(stderr, "wrote %s\n", path)
	}
	shaRoot := ""
	if !multi {
		// One root has one HEAD; a merged multi root baseline records none.
		shaRoot = roots[0]
	}
	code := guardExit(findings, guardOpts{
		root:         shaRoot,
		baselinePath: *baselinePath,
		update:       *updateBaseline,
		failOn:       *failOn,
		failOnSet:    failOnSet,
		maxAgeDays:   *maxAge,
		scanned:      only,
	}, stderr)
	// A blown budget is a findings failure, never an operational error,
	// and never masks one. Two runs stay exempt: recording a baseline,
	// whose contract is to write the file and exit clean, and --fail-on
	// none, which promises never to fail. The line prints either way.
	for _, line := range overBudgets {
		fmt.Fprintln(stderr, line)
		if code == ExitClean && !*updateBaseline && *failOn != "none" {
			code = ExitFindings
		}
	}
	return code
}

// defaultBaselinePath applies the conventional baseline location when
// the flag is unset.
func defaultBaselinePath(path string) string {
	if path == "" {
		return ".overwater.json"
	}
	return path
}

// gitHead returns the commit sha of the repository containing root, or
// "" when git or a repository is absent. Baselines record it so
// --incremental knows what to diff against. Local git only, no network.
func gitHead(root string) string {
	if root == "" {
		return ""
	}
	out, err := exec.Command("git", "-C", root, "rev-parse", "HEAD").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// gitChangedFiles lists the files changed since sha plus untracked
// files, relative to root. Any git failure is returned so the caller
// can fall back to a full scan.
func gitChangedFiles(root, sha string) (map[string]bool, error) {
	diff, err := exec.Command("git", "-C", root, "diff", "--relative", "--name-only", sha).Output()
	if err != nil {
		return nil, fmt.Errorf("git diff: %w", err)
	}
	untracked, err := exec.Command("git", "-C", root, "ls-files", "--others", "--exclude-standard").Output()
	if err != nil {
		return nil, fmt.Errorf("git ls-files: %w", err)
	}
	only := map[string]bool{}
	for _, name := range strings.Split(string(diff)+string(untracked), "\n") {
		if name = strings.TrimSpace(name); name != "" {
			only[name] = true
		}
	}
	return only, nil
}

// incrementalSet resolves --incremental into the set of files to scan.
// A nil result means full scan; the reason is already on stderr.
func incrementalSet(root, baselinePath string, stderr io.Writer) map[string]bool {
	bl, err := baseline.Load(baselinePath)
	if err != nil {
		fmt.Fprintf(stderr, "incremental: %v; scanning everything\n", err)
		return nil
	}
	if bl.Commit == "" {
		fmt.Fprintln(stderr, "incremental: baseline has no commit recorded; scanning everything")
		return nil
	}
	only, err := gitChangedFiles(root, bl.Commit)
	if err != nil {
		fmt.Fprintf(stderr, "incremental: %v; scanning everything\n", err)
		return nil
	}
	return only
}

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

// analyzeRepo keeps the one root pipeline shape eval uses. A blown
// budget is a guard concern; eval ignores it.
func analyzeRepo(root string, volume int, stderr io.Writer) (*catalog.Catalog, []rules.Finding, render.Meta, error) {
	p, err := newPipeline(volume, stderr)
	if err != nil {
		return nil, nil, render.Meta{}, err
	}
	pl, err := planRoot(root)
	if err != nil {
		return nil, nil, render.Meta{}, err
	}
	p.meta.CallsPerMonth = p.volumeFor(pl, volume)
	findings, _, err := p.scanRoot(pl, nil, p.meta.CallsPerMonth)
	if err != nil {
		return nil, nil, render.Meta{}, err
	}
	return p.cat, findings, p.meta, nil
}

// guardOpts carries everything guardExit needs beyond the findings.
type guardOpts struct {
	root         string // scanned root, for the baseline commit sha; "" skips it
	baselinePath string
	update       bool
	failOn       string
	failOnSet    bool
	maxAgeDays   int
	scanned      map[string]bool // non nil when the scan was incremental
}

// guardExit applies the failure policy. Recording a baseline never
// fails; findings fail only when the policy says so; and anything wrong
// with the baseline itself is an operational error, exit 2, never 1.
// Aged baseline entries nag on stderr and never move the exit code.
func guardExit(findings []rules.Finding, o guardOpts, stderr io.Writer) int {
	if o.update {
		path := defaultBaselinePath(o.baselinePath)
		entries := baseline.Entries(findings)
		if o.scanned != nil {
			// A partial scan keeps the entries it never looked at.
			if old, err := baseline.Load(path); err == nil {
				entries = append(entries, baseline.Outside(old, o.scanned)...)
			}
		}
		if err := baseline.Write(path, entries, gitHead(o.root)); err != nil {
			fmt.Fprintf(stderr, "overwater: %v\n", err)
			return ExitError
		}
		fmt.Fprintf(stderr, "wrote %s: %d findings baselined\n", path, len(entries))
		return ExitClean
	}
	// Aging nags run under every failure policy: a provided baseline
	// with --max-baseline-age-days set gets its aged entries named even
	// when the policy is any or none.
	var bl *baseline.File
	if o.baselinePath != "" && (o.failOn == "new" || o.maxAgeDays > 0) {
		var err error
		bl, err = baseline.Load(o.baselinePath)
		if err != nil {
			fmt.Fprintf(stderr, "overwater: %v\n", err)
			return ExitError
		}
		for _, a := range baseline.AgedMatches(findings, bl, time.Now(), o.maxAgeDays) {
			if a.Days < 0 {
				fmt.Fprintf(stderr, "baseline: %s at %s has an unreadable recorded date %q; re-record it\n",
					a.Entry.Rule, a.Entry.File, a.Entry.Recorded)
				continue
			}
			fmt.Fprintf(stderr, "baseline: %s at %s baselined %d days ago, past the %d day limit\n",
				a.Entry.Rule, a.Entry.File, a.Days, o.maxAgeDays)
		}
	}
	switch o.failOn {
	case "none":
		return ExitClean
	case "any":
		if len(findings) > 0 {
			fmt.Fprintf(stderr, "%d findings; failing under --fail-on any\n", len(findings))
			return ExitFindings
		}
		return ExitClean
	}
	// fail-on new
	if o.baselinePath == "" {
		if o.failOnSet {
			fmt.Fprintln(stderr, "overwater: --fail-on new needs --baseline; run once with --update-baseline to record one")
			return ExitError
		}
		// Advisor mode: no baseline, no explicit policy, no failure.
		return ExitClean
	}
	fresh := baseline.NewFindings(findings, bl)
	if len(fresh) > 0 {
		fmt.Fprintf(stderr, "%d findings, %d new against %s\n", len(findings), len(fresh), o.baselinePath)
		for _, f := range fresh {
			fmt.Fprintf(stderr, "  new: %s at %s:%d\n", f.RuleID, f.File, f.Line)
		}
		return ExitFindings
	}
	if len(findings) > 0 {
		fmt.Fprintf(stderr, "%d findings, all baselined in %s\n", len(findings), o.baselinePath)
	}
	return ExitClean
}

// runEval generates one A/B eval script per finding that nominates a
// different model. The user supplies prompts and keys and runs the
// scripts themselves; the scanner never does.
func runEval(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("eval", flag.ContinueOnError)
	fs.SetOutput(stderr)
	outDir := fs.String("o", "overwater-evals", "directory for generated scripts")
	volume := fs.Int("volume", 0, "estimated calls per month per call site")
	draft := fs.Bool("draft-prompts", false, "seed a prompts.jsonl per script from literals near the call site")
	if err := fs.Parse(args); err != nil {
		return ExitError
	}
	if fs.NArg() > 1 {
		fmt.Fprintln(stderr, "overwater: eval expects at most one path")
		return ExitError
	}
	root := "."
	if fs.NArg() == 1 {
		root = fs.Arg(0)
	}
	cat, findings, _, err := analyzeRepo(root, *volume, stderr)
	if err != nil {
		fmt.Fprintf(stderr, "overwater: %v\n", err)
		return ExitError
	}
	if len(findings) == 0 {
		fmt.Fprintf(stdout, "%s Nothing to eval.\n", render.KeepVerdict)
		return ExitClean
	}
	written, skipped, err := evalgen.Generate(findings, cat, *outDir)
	if err != nil {
		fmt.Fprintf(stderr, "overwater: %v\n", err)
		return ExitError
	}
	for _, path := range written {
		fmt.Fprintf(stdout, "wrote %s\n", path)
	}
	for _, note := range skipped {
		fmt.Fprintf(stderr, "skipped %s\n", note)
	}
	if *draft {
		drafts, err := evalgen.DraftPromptSets(root, findings, *outDir)
		if err != nil {
			fmt.Fprintf(stderr, "overwater: %v\n", err)
			return ExitError
		}
		for _, path := range drafts {
			fmt.Fprintf(stdout, "wrote %s (drafted; real production prompts beat these)\n", path)
		}
	}
	if len(written) == 0 {
		fmt.Fprintln(stdout, "no findings nominate a different model, so there is nothing to eval")
	}
	return ExitClean
}

func runCatalog(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		printCatalogUsage(stderr)
		return ExitError
	}
	switch args[0] {
	case "build":
		return runCatalogBuild(args[1:], stdout, stderr)
	case "show":
		return runCatalogShow(stdout, stderr)
	case "refresh":
		return runCatalogRefresh(args[1:], stdout, stderr)
	case "diff":
		return runCatalogDiff(args[1:], stdout, stderr)
	case "help", "-h", "--help":
		printCatalogUsage(stdout)
		return ExitClean
	}
	fmt.Fprintf(stderr, "overwater catalog: unknown subcommand %q\n", args[0])
	printCatalogUsage(stderr)
	return ExitError
}

func printCatalogUsage(w io.Writer) {
	fmt.Fprint(w, "Work with the model catalog.\n\nUsage:\n\n  overwater catalog <subcommand> [flags]\n\nSubcommands:\n\n")
	fmt.Fprint(w, "  build      validate the YAML entries and write catalog.json\n")
	fmt.Fprint(w, "  diff       compare entry prices against a litellm pricing file\n")
	fmt.Fprint(w, "  refresh    fetch the published catalog into the local cache\n")
	fmt.Fprint(w, "  show       print the models in the effective catalog\n\n")
}

// runCatalogDiff compares the catalog sources against a local copy of
// LiteLLM's pricing file, and with -write applies the drifted prices,
// bumps VERSION, and rebuilds catalog.json. Maintainer tooling; the
// nightly price-watch workflow drives it.
func runCatalogDiff(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("catalog diff", flag.ContinueOnError)
	fs.SetOutput(stderr)
	dir := fs.String("dir", "catalog", "catalog source directory")
	write := fs.Bool("write", false, "apply drifted prices and rebuild catalog.json")
	if err := fs.Parse(args); err != nil {
		return ExitError
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(stderr, "overwater: catalog diff expects one litellm pricing file path")
		return ExitError
	}
	raw, err := os.ReadFile(fs.Arg(0))
	if err != nil {
		fmt.Fprintf(stderr, "overwater: %v\n", err)
		return ExitError
	}
	prices, err := catalog.ParseLitellm(raw)
	if err != nil {
		fmt.Fprintf(stderr, "overwater: %v\n", err)
		return ExitError
	}
	c, err := catalog.LoadDir(*dir)
	if err != nil {
		fmt.Fprintf(stderr, "overwater: %v\n", err)
		return ExitError
	}
	drifts, notes, missing := catalog.DiffLitellm(c, prices)
	for _, d := range drifts {
		// An upstream record without an output price shows "?", never a
		// zero that reads like a real price.
		theirsOut := "?"
		if d.TheirsOutKnown {
			theirsOut = strconv.FormatFloat(d.TheirsOut, 'g', -1, 64)
		}
		fmt.Fprintf(stdout, "%s: ours %g/%g, litellm %g/%s\n", d.ID, d.OursIn, d.OursOut, d.TheirsIn, theirsOut)
	}
	for _, n := range notes {
		fmt.Fprintf(stdout, "note: %s\n", n)
	}
	fmt.Fprintf(stdout, "%d drifted, %d notes, %d not in litellm, %d checked\n", len(drifts), len(notes), len(missing), len(c.Models))
	if !*write || len(drifts) == 0 {
		return ExitClean
	}
	version := time.Now().Format("2006-01-02")
	if err := catalog.ApplyPrices(*dir, drifts, version); err != nil {
		fmt.Fprintf(stderr, "overwater: %v\n", err)
		return ExitError
	}
	fmt.Fprintf(stdout, "updated %d entries, bumped VERSION to %s, rebuilt catalog.json\n", len(drifts), version)
	return ExitClean
}

func runCatalogRefresh(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("catalog refresh", flag.ContinueOnError)
	fs.SetOutput(stderr)
	url := fs.String("url", catalog.DefaultURL, "catalog URL")
	offline := fs.Bool("offline", false, "forbid all network activity")
	if err := fs.Parse(args); err != nil {
		return ExitError
	}
	if *offline {
		fmt.Fprintln(stderr, "overwater: refresh is a network operation and --offline forbids it")
		return ExitError
	}
	c, raw, err := catalog.Fetch(httpClient, *url)
	if err != nil {
		fmt.Fprintf(stderr, "overwater: %v\n", err)
		return ExitError
	}
	path, err := catalog.WriteCache(raw)
	if err != nil {
		fmt.Fprintf(stderr, "overwater: %v\n", err)
		return ExitError
	}
	fmt.Fprintf(stdout, "cached catalog %s: %d models (%s)\n", c.Version, len(c.Models), path)
	return ExitClean
}

func runCatalogBuild(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("catalog build", flag.ContinueOnError)
	fs.SetOutput(stderr)
	dir := fs.String("dir", "catalog", "catalog source directory")
	out := fs.String("o", "", "output path (default <dir>/catalog.json)")
	if err := fs.Parse(args); err != nil {
		return ExitError
	}
	c, err := catalog.LoadDir(*dir)
	if err != nil {
		fmt.Fprintf(stderr, "overwater: %v\n", err)
		return ExitError
	}
	b, err := c.JSON()
	if err != nil {
		fmt.Fprintf(stderr, "overwater: %v\n", err)
		return ExitError
	}
	path := *out
	if path == "" {
		path = filepath.Join(*dir, "catalog.json")
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		fmt.Fprintf(stderr, "overwater: %v\n", err)
		return ExitError
	}
	fmt.Fprintf(stdout, "wrote %s: %d models, prices as of %s\n", path, len(c.Models), c.Version)
	return ExitClean
}

func runCatalogShow(stdout, stderr io.Writer) int {
	c, note, err := catalog.Effective()
	if err != nil {
		fmt.Fprintf(stderr, "overwater: %v\n", err)
		return ExitError
	}
	if note != "" {
		fmt.Fprintln(stderr, note)
	}
	fmt.Fprintf(stdout, "catalog %s: %d models\n\n", c.Version, len(c.Models))
	w := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tPROVIDER\tTIER\tIN $/MTOK\tOUT $/MTOK\tSTATUS")
	for _, m := range c.Models {
		status := "active"
		if m.Deprecated != "" {
			status = "deprecated " + m.Deprecated
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%.2f\t%.2f\t%s\n", m.ID, m.Provider, m.Tier, m.InputPerMtok, m.OutputPerMtok, status)
	}
	w.Flush()
	return ExitClean
}
