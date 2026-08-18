package cli

import (
	"fmt"
	"io"
	"path/filepath"
	"strings"
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

// dropBaselineFile removes the findings a baseline makes about itself.
// The walker skips .overwater.json by name, but a baseline kept
// anywhere else inside the tree it guards is read as source: its
// entries name model ids, those come back as findings, and recording
// them writes more entries still, so the ratchet grows by a finding a
// run and the build can never go green. A merged multi root run carries
// no single root to resolve the path against, and prefixes its findings
// with the root name, so it is left alone.
func dropBaselineFile(findings []rules.Finding, root, baselinePath string) []rules.Finding {
	if root == "" || baselinePath == "" {
		return findings
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return findings
	}
	absBaseline, err := filepath.Abs(baselinePath)
	if err != nil {
		return findings
	}
	rel, err := filepath.Rel(absRoot, absBaseline)
	if err != nil {
		return findings
	}
	rel = filepath.ToSlash(rel)
	if rel == ".." || strings.HasPrefix(rel, "../") {
		return findings // outside the scanned tree, so never walked
	}
	kept := make([]rules.Finding, 0, len(findings))
	for _, f := range findings {
		if f.File != rel {
			kept = append(kept, f)
		}
	}
	return kept
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
func nagAged(findings []rules.Finding, bl *baseline.File, o guardOpts, stderr io.Writer) {
	for _, a := range baseline.AgedMatches(findings, bl, o.scanned, time.Now(), o.maxAgeDays) {
		if a.Days < 0 {
			fmt.Fprintf(stderr, "baseline: %s at %s has an unreadable recorded date %q; re-record it\n",
				a.Entry.Rule, a.Entry.File, a.Entry.Recorded)
			continue
		}
		fmt.Fprintf(stderr, "baseline: %s at %s baselined %d days ago, past the %d day limit\n",
			a.Entry.Rule, a.Entry.File, a.Days, o.maxAgeDays)
	}
}

// guardExit applies the failure policy. Anything wrong with the
// baseline itself is exit 2, never 1.
func guardExit(findings []rules.Finding, o guardOpts, stderr io.Writer) int {
	findings = dropBaselineFile(findings, o.root, o.baselinePath)
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
		nagAged(findings, bl, o, stderr)
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
			// Recording is only half the remedy: --update-baseline writes
			// the default path but nothing reads it back without
			// --baseline, so a message naming only the first half sends
			// the user around the same exit 2 again.
			bl := defaultBaselinePath("")
			fmt.Fprintf(stderr, "overwater: --fail-on new needs --baseline; run once with --update-baseline to record %s, then pass --baseline %s\n", bl, bl)
			return ExitError
		}
		// Advisor mode: no baseline, no explicit policy, no failure.
		return ExitClean
	}
	fresh := baseline.NewFindings(findings, bl, o.scanned)
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
