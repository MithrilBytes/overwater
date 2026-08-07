package cli

import (
	"fmt"
	"io"
	"time"

	"github.com/MithrilBytes/overwater/internal/baseline"
	"github.com/MithrilBytes/overwater/rules"
)

func defaultBaselinePath(path string) string {
	if path == "" {
		return ".overwater.json"
	}
	return path
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

// recordBaseline writes this scan's findings as the new baseline.
// Recording never fails the build; only a bad write is exit 2.
func recordBaseline(findings []rules.Finding, o guardOpts, stderr io.Writer) int {
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

// nagAged names the baseline entries that have outlived the age limit.
// Nags never move the exit code.
func nagAged(findings []rules.Finding, bl *baseline.File, maxDays int, stderr io.Writer) {
	for _, a := range baseline.AgedMatches(findings, bl, time.Now(), maxDays) {
		if a.Days < 0 {
			fmt.Fprintf(stderr, "baseline: %s at %s has an unreadable recorded date %q; re-record it\n",
				a.Entry.Rule, a.Entry.File, a.Entry.Recorded)
			continue
		}
		fmt.Fprintf(stderr, "baseline: %s at %s baselined %d days ago, past the %d day limit\n",
			a.Entry.Rule, a.Entry.File, a.Days, maxDays)
	}
}

// guardExit applies the failure policy. Anything wrong with the
// baseline itself is exit 2, never 1.
func guardExit(findings []rules.Finding, o guardOpts, stderr io.Writer) int {
	if o.update {
		return recordBaseline(findings, o, stderr)
	}
	// Aging nags run under every failure policy, not just fail-on new.
	var bl *baseline.File
	if o.baselinePath != "" && (o.failOn == "new" || o.maxAgeDays > 0) {
		var err error
		bl, err = baseline.Load(o.baselinePath)
		if err != nil {
			fmt.Fprintf(stderr, "overwater: %v\n", err)
			return ExitError
		}
		nagAged(findings, bl, o.maxAgeDays, stderr)
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
