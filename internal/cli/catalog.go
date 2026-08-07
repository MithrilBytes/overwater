package cli

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"text/tabwriter"
	"time"

	"github.com/MithrilBytes/overwater/catalog"
)

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
