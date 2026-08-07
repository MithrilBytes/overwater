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
