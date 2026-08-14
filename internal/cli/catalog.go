package cli

import (
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
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
	case "history":
		return runCatalogHistory(args[1:], stdout, stderr)
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
	fmt.Fprint(w, "  history    read the dated price snapshots\n")
	fmt.Fprint(w, "  refresh    fetch the published catalog into the local cache\n")
	fmt.Fprint(w, "  show       print the models in the effective catalog\n\n")
}

// runCatalogDiff compares the catalog sources against a local copy of
// LiteLLM's pricing file; -write applies the drifted prices, bumps
// VERSION, and rebuilds catalog.json. Maintainer tooling, run by the
// nightly price-watch workflow.
func runCatalogDiff(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("catalog diff", flag.ContinueOnError)
	fs.SetOutput(stderr)
	dir := fs.String("dir", "catalog", "catalog source directory")
	write := fs.Bool("write", false, "apply drifted prices and rebuild catalog.json")
	reverse := fs.Bool("reverse", false, "list models litellm prices that this catalog lacks")
	only := fs.String("only", "", "comma separated ids to narrow -reverse to")
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
	if *reverse {
		var ids []string
		if *only != "" {
			for _, id := range strings.Split(*only, ",") {
				if id = strings.TrimSpace(id); id != "" {
					ids = append(ids, id)
				}
			}
		}
		unlisted := catalog.ReverseDiff(c, prices, ids)
		for _, u := range unlisted {
			// Upstream stores dollars per token, so a price that is a
			// round number per million arrives as 3.1999999999999997.
			price := func(v float64) string {
				return strconv.FormatFloat(math.Round(v*1e6)/1e6, 'f', -1, 64)
			}
			out := "?"
			if u.HasOutput {
				out = price(u.Output)
			}
			fmt.Fprintf(stdout, "%s\t%s\t%s\tin %s\tout %s\tctx %d\t%d routes\n",
				u.ID, u.Provider, u.Mode, price(u.Input), out, u.MaxInput, len(u.Keys))
		}
		// The count goes to stderr so stdout is a clean list a caller can
		// diff against the last one it saw.
		fmt.Fprintf(stderr, "%d models priced upstream and absent here\n", len(unlisted))
		return ExitClean
	}
	drifts, notes, missing := catalog.DiffLitellm(c, prices)
	for _, d := range drifts {
		// A missing upstream output price prints "?", never a zero that
		// reads like a real price.
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

// runCatalogHistory reads the dated snapshots that catalog diff -write
// leaves under history/: one model's price over time, what moved on one
// date, or the snapshot list when given neither.
func runCatalogHistory(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("catalog history", flag.ContinueOnError)
	fs.SetOutput(stderr)
	dir := fs.String("dir", "catalog", "catalog source directory")
	model := fs.String("model", "", "id or alias to price over time")
	on := fs.String("on", "", "snapshot date (YYYY-MM-DD) to report the change for")
	if err := fs.Parse(args); err != nil {
		return ExitError
	}
	// Answering both at once would be a third report, not these two.
	if *model != "" && *on != "" {
		fmt.Fprintln(stderr, "overwater: catalog history takes -model or -on, not both")
		return ExitError
	}
	snaps, err := catalog.LoadHistory(*dir)
	if err != nil {
		fmt.Fprintf(stderr, "overwater: %v\n", err)
		return ExitError
	}
	if len(snaps) == 0 {
		fmt.Fprintf(stderr, "overwater: no price history under %s\n", filepath.Join(*dir, "history"))
		return ExitError
	}
	if note := historyNote(*dir, snaps); note != "" {
		fmt.Fprintln(stderr, note)
	}
	switch {
	case *model != "":
		return printModelHistory(stdout, stderr, snaps, *model)
	case *on != "":
		return printSnapshotChange(stdout, stderr, snaps, *on)
	}
	span := snaps[0].Catalog.Version
	if last := snaps[len(snaps)-1].Catalog.Version; last != span {
		span += " to " + last
	}
	fmt.Fprintf(stdout, "%d %s, %s\n\n", len(snaps), plural(len(snaps), "snapshot", "snapshots"), span)
	w := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "DATE\tMODELS\tPRICES MOVED")
	for i, s := range snaps {
		moved := "-" // nothing before the first snapshot to have moved from
		if i > 0 {
			moved = strconv.Itoa(pricesMoved(snaps[i-1].Catalog, s.Catalog))
		}
		fmt.Fprintf(w, "%s\t%d\t%s\n", s.Catalog.Version, len(s.Catalog.Models), moved)
	}
	w.Flush()
	return ExitClean
}

// pricesMoved counts the entries whose price changed, leaving out the
// ones that arrived or left: the model count column already shows those.
func pricesMoved(prev, cur *catalog.Catalog) int {
	n := 0
	for _, ch := range catalog.ChangesBetween(prev, cur) {
		if !ch.Added && !ch.Dropped {
			n++
		}
	}
	return n
}

// historyNote warns when the working catalog is newer than the last
// snapshot. Only an applied price writes history, so a hand edited price
// with a bumped VERSION leaves the series short of today, and the last
// row would otherwise read as the current price.
func historyNote(dir string, snaps []catalog.Snapshot) string {
	raw, err := os.ReadFile(filepath.Join(dir, "VERSION"))
	if err != nil {
		return "" // a directory of snapshots alone is a fair thing to read
	}
	version := strings.TrimSpace(string(raw))
	last := snaps[len(snaps)-1].Catalog.Version
	if version <= last {
		return ""
	}
	return fmt.Sprintf("catalog VERSION is %s, newer than the last snapshot %s: the series stops short of the current prices", version, last)
}

func printModelHistory(stdout, stderr io.Writer, snaps []catalog.Snapshot, name string) int {
	points := catalog.Series(snaps, name)
	if len(points) == 0 {
		fmt.Fprintf(stderr, "overwater: no snapshot from %s to %s carries %q\n",
			snaps[0].Catalog.Version, snaps[len(snaps)-1].Catalog.Version, name)
		return ExitError
	}
	// An alias upstream repointed resolves to different entries along the
	// series, and the price step means nothing without saying which.
	showID := false
	for _, p := range points {
		if p.ID != name {
			showID = true
		}
	}
	fmt.Fprintf(stdout, "%s across %d %s\n\n", name, len(points), plural(len(points), "snapshot", "snapshots"))
	w := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	header, row := "DATE\tIN $/MTOK\tOUT $/MTOK", "%s\t%g\t%g"
	if showID {
		header, row = header+"\tID", row+"\t%s"
	}
	fmt.Fprintln(w, header)
	for _, p := range points {
		if showID {
			fmt.Fprintf(w, row+"\n", p.Version, p.In, p.Out, p.ID)
			continue
		}
		fmt.Fprintf(w, row+"\n", p.Version, p.In, p.Out)
	}
	w.Flush()
	first, last := points[0], points[len(points)-1]
	move := priceMove(catalog.PriceChange{
		OldIn: first.In, OldOut: first.Out, NewIn: last.In, NewOut: last.Out,
	})
	if move == "" {
		fmt.Fprintf(stdout, "\nunchanged at %g/%g since %s\n", first.In, first.Out, first.Version)
		return ExitClean
	}
	fmt.Fprintf(stdout, "\n%s between %s and %s\n", move, first.Version, last.Version)
	return ExitClean
}

func printSnapshotChange(stdout, stderr io.Writer, snaps []catalog.Snapshot, date string) int {
	at := -1
	for i, s := range snaps {
		if s.Catalog.Version == date {
			at = i
			break
		}
	}
	if at < 0 {
		fmt.Fprintf(stderr, "overwater: no snapshot dated %s. Snapshots are:\n\n", date)
		for _, s := range snaps {
			fmt.Fprintf(stderr, "  %s\n", s.Catalog.Version)
		}
		fmt.Fprintln(stderr)
		return ExitError
	}
	cur := snaps[at].Catalog
	if at == 0 {
		fmt.Fprintf(stdout, "%s is the earliest snapshot: %d models, nothing before it to compare\n",
			cur.Version, len(cur.Models))
		return ExitClean
	}
	prev := snaps[at-1].Catalog
	fmt.Fprintf(stdout, "%s against %s\n\n", cur.Version, prev.Version)
	moved, added, dropped := 0, 0, 0
	for _, ch := range catalog.ChangesBetween(prev, cur) {
		switch {
		case ch.Added:
			added++
			fmt.Fprintf(stdout, "%s: added at %g/%g\n", ch.ID, ch.NewIn, ch.NewOut)
		case ch.Dropped:
			dropped++
			fmt.Fprintf(stdout, "%s: gone, last priced %g/%g\n", ch.ID, ch.OldIn, ch.OldOut)
		default:
			moved++
			fmt.Fprintf(stdout, "%s: %s\n", ch.ID, priceMove(ch))
		}
	}
	fmt.Fprintf(stdout, "%d moved, %d added, %d dropped\n", moved, added, dropped)
	return ExitClean
}

// priceMove names only the side that moved: an output price rewritten
// under an input price that held is its own event.
func priceMove(ch catalog.PriceChange) string {
	var parts []string
	if ch.OldIn != ch.NewIn {
		parts = append(parts, fmt.Sprintf("in %g -> %g", ch.OldIn, ch.NewIn))
	}
	if ch.OldOut != ch.NewOut {
		parts = append(parts, fmt.Sprintf("out %g -> %g", ch.OldOut, ch.NewOut))
	}
	return strings.Join(parts, ", ")
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
