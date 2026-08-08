package cli

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/MithrilBytes/overwater/catalog"
	"github.com/MithrilBytes/overwater/internal/render"
	"github.com/MithrilBytes/overwater/rules"
)

type scanFlags struct {
	roots          []string
	volume         int
	volumesPath    string
	jsonOut        bool
	summary        bool
	sarif          string
	html           string
	csv            string
	modelsMD       bool
	baselinePath   string
	updateBaseline bool
	maxAgeDays     int
	failOn         string
	failOnSet      bool
	incremental    bool
	refresh        bool
	offline        bool
}

func (f scanFlags) multi() bool { return len(f.roots) > 1 }

// parseScanFlags reports usage errors on stderr; false means exit 2.
func parseScanFlags(args []string, stderr io.Writer) (scanFlags, bool) {
	var f scanFlags
	fs := flag.NewFlagSet("scan", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		fmt.Fprint(stderr, "Usage: overwater scan [flags] [path...]\n\n")
		fmt.Fprint(stderr, "A path is a directory or a single file. A file is scanned with its\n")
		fmt.Fprint(stderr, "containing directory as context, and that directory's .overwater.yaml\n")
		fmt.Fprint(stderr, "applies; only the named file reports findings.\n\nFlags:\n")
		fs.PrintDefaults()
	}
	fs.BoolVar(&f.jsonOut, "json", false, "emit findings as JSON instead of text")
	fs.StringVar(&f.sarif, "sarif", "", "write findings as SARIF 2.1.0 to this path")
	fs.BoolVar(&f.modelsMD, "models-md", false, "write MODELS.md into the scanned repo")
	fs.IntVar(&f.volume, "volume", 0, "estimated calls per month per call site")
	fs.StringVar(&f.volumesPath, "volumes", "", "JSON file of measured monthly calls by call site or model")
	fs.StringVar(&f.baselinePath, "baseline", "", "baseline file for the ratchet")
	fs.BoolVar(&f.updateBaseline, "update-baseline", false, "record this scan's findings as the baseline")
	fs.IntVar(&f.maxAgeDays, "max-baseline-age-days", 0, "nag when a matched baseline entry is older than this many days (0 disables)")
	fs.BoolVar(&f.incremental, "incremental", false, "scan only files changed since the baseline's recorded commit")
	fs.StringVar(&f.failOn, "fail-on", "new", "failure policy: new, any, or none (none never fails, even over budget)")
	fs.BoolVar(&f.refresh, "refresh", false, "fetch the published catalog before scanning")
	fs.BoolVar(&f.offline, "offline", false, "forbid all network activity")
	fs.StringVar(&f.html, "html", "", "write a single file HTML report to this path")
	fs.StringVar(&f.csv, "csv", "", "write findings as CSV to this path")
	fs.BoolVar(&f.summary, "summary", false, "print a one line summary instead of the full verdict")
	if err := fs.Parse(args); err != nil {
		return f, false
	}
	fs.Visit(func(fl *flag.Flag) {
		if fl.Name == "fail-on" {
			f.failOnSet = true
		}
	})
	if f.failOn != "new" && f.failOn != "any" && f.failOn != "none" {
		fmt.Fprintf(stderr, "overwater: unknown --fail-on %q, want new, any, or none\n", f.failOn)
		return f, false
	}
	f.roots = fs.Args()
	if len(f.roots) == 0 {
		f.roots = []string{"."}
	}
	if f.multi() && f.modelsMD {
		fmt.Fprintln(stderr, "overwater: --models-md needs a single root")
		return f, false
	}
	return f, true
}

// runScan prints the findings; scanExit decides what they do to the
// exit code.
func runScan(args []string, stdout, stderr io.Writer) int {
	f, ok := parseScanFlags(args, stderr)
	if !ok {
		return ExitError
	}
	if f.refresh {
		refreshCatalog(f.offline, stderr)
	}
	plans, err := planRoots(f.roots)
	if err != nil {
		fmt.Fprintf(stderr, "overwater: %v\n", err)
		return ExitError
	}
	if f.modelsMD && plans[0].only != nil {
		fmt.Fprintln(stderr, "overwater: --models-md needs a directory root, not a file")
		return ExitError
	}
	var only map[string]bool
	if f.incremental {
		switch {
		case f.multi():
			fmt.Fprintln(stderr, "incremental: multiple roots; scanning everything")
		case plans[0].only != nil:
			fmt.Fprintln(stderr, "incremental: file root; scanning only the named file")
		default:
			only = incrementalSet(plans[0].root, defaultBaselinePath(f.baselinePath), stderr)
		}
	}
	var vols *volumesFile
	if f.volumesPath != "" {
		loaded, err := loadVolumes(f.volumesPath)
		if err != nil {
			fmt.Fprintf(stderr, "overwater: %v\n", err)
			return ExitError
		}
		vols = loaded
	}
	p, err := newPipeline(f.volume, vols, stderr)
	if err != nil {
		fmt.Fprintf(stderr, "overwater: %v\n", err)
		return ExitError
	}
	vol := p.volumeAcross(plans, f.volume, stderr)
	p.meta.CallsPerMonth = vol.calls
	findings, overBudgets, err := p.scanPlans(plans, only, vol, stderr)
	if err != nil {
		fmt.Fprintf(stderr, "overwater: %v\n", err)
		return ExitError
	}
	if only != nil {
		noteCoverage(plans[0].root, only, stderr)
	}
	if err := writeVerdict(f, findings, p.meta, stdout); err != nil {
		fmt.Fprintf(stderr, "overwater: %v\n", err)
		return ExitError
	}
	if err := writeReports(f, findings, p.meta, stderr); err != nil {
		fmt.Fprintf(stderr, "overwater: %v\n", err)
		return ExitError
	}
	return scanExit(f, plans[0].root, findings, only, overBudgets, stderr)
}

// refreshCatalog fetches the published catalog into the cache. Failure
// is advisory: the scan carries on with local prices.
func refreshCatalog(offline bool, stderr io.Writer) {
	if offline {
		fmt.Fprintln(stderr, "offline: skipping catalog refresh")
		return
	}
	fetched, raw, err := catalog.Fetch(httpClient, catalog.DefaultURL)
	if err != nil {
		fmt.Fprintf(stderr, "catalog refresh failed: %v; scanning with local prices\n", err)
		return
	}
	if _, err := catalog.WriteCache(raw); err != nil {
		fmt.Fprintf(stderr, "could not cache catalog %s: %v\n", fetched.Version, err)
	}
}

// noteCoverage reports how much a restricted scan covered, so a null
// verdict over zero files cannot read as clean. Changed files that no
// longer exist were never scanned.
func noteCoverage(root string, only map[string]bool, stderr io.Writer) {
	scanned := 0
	for f := range only {
		if info, err := os.Stat(filepath.Join(root, filepath.FromSlash(f))); err == nil && info.Mode().IsRegular() {
			scanned++
		}
	}
	fmt.Fprintf(stderr, "incremental: scanned %d of %d candidate files\n", scanned, len(only))
}

// writeVerdict prints the report in the shape the flags asked for.
func writeVerdict(f scanFlags, findings []rules.Finding, meta render.Meta, stdout io.Writer) error {
	switch {
	case f.jsonOut:
		out, err := render.JSON(findings, meta)
		if err != nil {
			return err
		}
		stdout.Write(out)
	case f.summary:
		fmt.Fprintln(stdout, render.SummaryLine(findings, meta))
	default:
		render.Terminal(stdout, findings, meta)
	}
	return nil
}

// writeReports writes the file surfaces the flags asked for.
func writeReports(f scanFlags, findings []rules.Finding, meta render.Meta, stderr io.Writer) error {
	if f.html != "" {
		if err := writeReport(f.html, render.HTML(findings, meta), stderr); err != nil {
			return err
		}
	}
	if f.csv != "" {
		if err := writeReport(f.csv, render.CSV(findings), stderr); err != nil {
			return err
		}
	}
	if f.sarif != "" {
		out, err := render.SARIF(findings, meta)
		if err != nil {
			return err
		}
		if err := writeReport(f.sarif, out, stderr); err != nil {
			return err
		}
	}
	if f.modelsMD {
		if err := writeReport(filepath.Join(f.roots[0], "MODELS.md"), render.ModelsMD(findings, meta), stderr); err != nil {
			return err
		}
	}
	return nil
}

func writeReport(path string, data []byte, stderr io.Writer) error {
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return err
	}
	fmt.Fprintf(stderr, "wrote %s\n", path)
	return nil
}

// scanExit is the guard's verdict plus the budget lines. A blown budget
// exits 1, never 2, and never masks a 2. Recording a baseline and
// --fail-on none are exempt; the line prints either way.
func scanExit(f scanFlags, root string, findings []rules.Finding, only map[string]bool, overBudgets []string, stderr io.Writer) int {
	shaRoot := ""
	if !f.multi() {
		// One root has one HEAD; a merged multi root baseline records none.
		shaRoot = root
	}
	code := guardExit(findings, guardOpts{
		root:         shaRoot,
		baselinePath: f.baselinePath,
		update:       f.updateBaseline,
		failOn:       f.failOn,
		failOnSet:    f.failOnSet,
		maxAgeDays:   f.maxAgeDays,
		scanned:      only,
	}, stderr)
	for _, line := range overBudgets {
		fmt.Fprintln(stderr, line)
		if code == ExitClean && !f.updateBaseline && f.failOn != "none" {
			code = ExitFindings
		}
	}
	return code
}
