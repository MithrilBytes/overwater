package cli

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
)

// readRepoList reads a fleet list file: one repo path per line, blank
// lines and # comments skipped.
func readRepoList(path string) ([]string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var repos []string
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		repos = append(repos, line)
	}
	return repos, nil
}

// runFleet scans every repository named in a list file with the default
// advisor settings: one stdout line per repo, then a rollup. A repo
// that fails is a stderr line and the run continues; only an unreadable
// list file is operational.
func runFleet(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("fleet", flag.ContinueOnError)
	fs.SetOutput(stderr)
	failOn := fs.String("fail-on", "none", "failure policy across the fleet: any or none")
	if err := fs.Parse(args); err != nil {
		return ExitError
	}
	if *failOn != "any" && *failOn != "none" {
		fmt.Fprintf(stderr, "overwater: unknown --fail-on %q, want any or none\n", *failOn)
		return ExitError
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(stderr, "overwater: fleet expects one list file: overwater fleet LISTFILE (one repo path per line, # comments)")
		return ExitError
	}
	repos, err := readRepoList(fs.Arg(0))
	if err != nil {
		fmt.Fprintf(stderr, "overwater: %v\n", err)
		return ExitError
	}
	p, err := newPipeline(0, stderr)
	if err != nil {
		fmt.Fprintf(stderr, "overwater: %v\n", err)
		return ExitError
	}
	scanned, failed, totalFindings, totalUSD := 0, 0, 0, 0
	for _, repo := range repos {
		// Each repo is planned on its own, so volume and disabled rules
		// never leak between fleet lines.
		pl, err := planRoot(repo)
		if err != nil {
			fmt.Fprintf(stderr, "overwater: %v\n", err)
			failed++
			continue
		}
		findings, overBudget, err := p.scanRoot(pl, nil, p.volumeFor(pl, 0))
		if err != nil {
			fmt.Fprintf(stderr, "overwater: %v\n", err)
			failed++
			continue
		}
		if overBudget != "" {
			fmt.Fprintf(stderr, "%s: %s\n", repo, overBudget)
		}
		usd := 0
		for _, f := range findings {
			usd += f.MonthlyUSD
		}
		fmt.Fprintf(stdout, "%s: %d findings, ~$%d/mo\n", repo, len(findings), usd)
		scanned++
		totalFindings += len(findings)
		totalUSD += usd
	}
	rollup := fmt.Sprintf("fleet: %d repos, %d findings, ~$%d/mo", scanned, totalFindings, totalUSD)
	if failed > 0 {
		rollup += fmt.Sprintf(", %d failed", failed)
	}
	fmt.Fprintln(stdout, rollup)
	// A fleet that scanned nothing learned nothing: that is operational.
	if len(repos) > 0 && scanned == 0 {
		fmt.Fprintf(stderr, "overwater: all %d repos failed to scan\n", failed)
		return ExitError
	}
	if *failOn == "any" && totalFindings > 0 {
		fmt.Fprintf(stderr, "%d findings across the fleet; failing under --fail-on any\n", totalFindings)
		return ExitFindings
	}
	return ExitClean
}
