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
	"path/filepath"
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

// A command is one subcommand of the overwater binary.
type command struct {
	name    string
	summary string
	run     func(args []string, stdout, stderr io.Writer) int
}

// commands is the router table. Order here is the order printed in usage.
var commands = []command{
	{"scan", "report overwatered LLM call sites in a repository", runScan},
	{"eval", "generate A/B eval scripts for scan findings", runEval},
	{"catalog", "show or refresh the model catalog", runCatalog},
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
	modelsMD := fs.Bool("models-md", false, "write MODELS.md into the scanned repo")
	volume := fs.Int("volume", 0, "estimated calls per month per call site")
	baselinePath := fs.String("baseline", "", "baseline file for the ratchet")
	updateBaseline := fs.Bool("update-baseline", false, "record this scan's findings as the baseline")
	failOn := fs.String("fail-on", "new", "failure policy: new, any, or none")
	refresh := fs.Bool("refresh", false, "fetch the published catalog before scanning")
	offline := fs.Bool("offline", false, "forbid all network activity")
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
	if fs.NArg() > 1 {
		fmt.Fprintln(stderr, "overwater: scan expects at most one path")
		return ExitError
	}
	root := "."
	if fs.NArg() == 1 {
		root = fs.Arg(0)
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
	_, findings, meta, err := analyzeRepo(root, *volume, stderr)
	if err != nil {
		fmt.Fprintf(stderr, "overwater: %v\n", err)
		return ExitError
	}
	if *jsonOut {
		out, err := render.JSON(findings, meta)
		if err != nil {
			fmt.Fprintf(stderr, "overwater: %v\n", err)
			return ExitError
		}
		stdout.Write(out)
	} else {
		render.Terminal(stdout, findings, meta)
	}
	if *modelsMD {
		path := filepath.Join(root, "MODELS.md")
		if err := os.WriteFile(path, render.ModelsMD(findings, meta), 0o644); err != nil {
			fmt.Fprintf(stderr, "overwater: %v\n", err)
			return ExitError
		}
		fmt.Fprintf(stderr, "wrote %s\n", path)
	}
	return guardExit(findings, *baselinePath, *updateBaseline, *failOn, failOnSet, stderr)
}

// analyzeRepo runs the shared pipeline for scan and eval: pick the
// effective catalog (embedded or a newer cache, zero network), load the
// rules, scan, evaluate. Advisory notes such as a bad cache or stale
// prices go to stderr; stdout belongs to the renderers.
func analyzeRepo(root string, volume int, stderr io.Writer) (*catalog.Catalog, []rules.Finding, render.Meta, error) {
	cat, note, err := catalog.Effective()
	if err != nil {
		return nil, nil, render.Meta{}, err
	}
	if note != "" {
		fmt.Fprintln(stderr, note)
	}
	if warn := catalog.Stale(cat, time.Now()); warn != "" {
		fmt.Fprintln(stderr, warn)
	}
	engine, err := rules.Load()
	if err != nil {
		return nil, nil, render.Meta{}, err
	}
	if volume > 0 {
		engine.Est.Volume.CallsPerMonth = volume
	}
	report, err := scan.Analyze(root, cat)
	if err != nil {
		return nil, nil, render.Meta{}, err
	}
	meta := render.Meta{
		CatalogVersion: cat.Version,
		CallsPerMonth:  engine.Est.Volume.CallsPerMonth,
	}
	return cat, engine.Evaluate(report, cat), meta, nil
}

// guardExit applies the failure policy. Recording a baseline never
// fails; findings fail only when the policy says so; and anything wrong
// with the baseline itself is an operational error, exit 2, never 1.
func guardExit(findings []rules.Finding, baselinePath string, update bool, failOn string, failOnSet bool, stderr io.Writer) int {
	if update {
		path := baselinePath
		if path == "" {
			path = ".overwater.json"
		}
		if err := baseline.Write(path, findings); err != nil {
			fmt.Fprintf(stderr, "overwater: %v\n", err)
			return ExitError
		}
		fmt.Fprintf(stderr, "wrote %s: %d findings baselined\n", path, len(findings))
		return ExitClean
	}
	switch failOn {
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
	if baselinePath == "" {
		if failOnSet {
			fmt.Fprintln(stderr, "overwater: --fail-on new needs --baseline; run once with --update-baseline to record one")
			return ExitError
		}
		// Advisor mode: no baseline, no explicit policy, no failure.
		return ExitClean
	}
	bl, err := baseline.Load(baselinePath)
	if err != nil {
		fmt.Fprintf(stderr, "overwater: %v\n", err)
		return ExitError
	}
	fresh := baseline.NewFindings(findings, bl)
	if len(fresh) > 0 {
		fmt.Fprintf(stderr, "%d findings, %d new against %s\n", len(findings), len(fresh), baselinePath)
		for _, f := range fresh {
			fmt.Fprintf(stderr, "  new: %s at %s:%d\n", f.RuleID, f.File, f.Line)
		}
		return ExitFindings
	}
	if len(findings) > 0 {
		fmt.Fprintf(stderr, "%d findings, all baselined in %s\n", len(findings), baselinePath)
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
	fmt.Fprint(w, "  refresh    fetch the published catalog into the local cache\n")
	fmt.Fprint(w, "  show       print the models in the effective catalog\n\n")
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
