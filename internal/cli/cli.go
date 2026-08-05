// Package cli routes overwater's subcommands and owns the exit code
// contract. CI scripts branch on these codes, so findings (1) and
// operational errors (2) are never conflated.
package cli

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"text/tabwriter"

	"github.com/MithrilBytes/overwater/catalog"
	"github.com/MithrilBytes/overwater/internal/render"
	"github.com/MithrilBytes/overwater/internal/scan"
	"github.com/MithrilBytes/overwater/rules"
)

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
	if err := fs.Parse(args); err != nil {
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
	cat, err := catalog.Embedded()
	if err != nil {
		fmt.Fprintf(stderr, "overwater: %v\n", err)
		return ExitError
	}
	engine, err := rules.Load()
	if err != nil {
		fmt.Fprintf(stderr, "overwater: %v\n", err)
		return ExitError
	}
	if *volume > 0 {
		engine.Est.Volume.CallsPerMonth = *volume
	}
	report, err := scan.Analyze(root, cat)
	if err != nil {
		fmt.Fprintf(stderr, "overwater: %v\n", err)
		return ExitError
	}
	findings := engine.Evaluate(report, cat)
	meta := render.Meta{
		CatalogVersion: cat.Version,
		CallsPerMonth:  engine.Est.Volume.CallsPerMonth,
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
	return ExitClean
}

func runEval(_ []string, _, stderr io.Writer) int {
	fmt.Fprintln(stderr, "overwater: eval is not implemented yet")
	return ExitError
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
	fmt.Fprint(w, "  build    validate the YAML entries and write catalog.json\n")
	fmt.Fprint(w, "  show     print the models in the embedded catalog\n\n")
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
	c, err := catalog.Embedded()
	if err != nil {
		fmt.Fprintf(stderr, "overwater: %v\n", err)
		return ExitError
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
