// Package cli routes overwater's subcommands. CI branches on the exit
// codes, so findings (1) and operational errors (2) never mix.
package cli

import (
	"fmt"
	"io"
	"net/http"
	"time"
)

// httpClient is the binary's only network path, and only the catalog
// fetch uses it.
var httpClient = &http.Client{Timeout: 15 * time.Second}

const (
	// ExitClean means nothing violates the failure policy.
	ExitClean = 0
	// ExitFindings means findings violate the failure policy.
	ExitFindings = 1
	// ExitError means the run itself failed: bad invocation, unreadable
	// repo, invalid baseline, malformed catalog.
	ExitError = 2
)

// buildVersion is stamped by the release workflow via ldflags.
var buildVersion = "dev"

type command struct {
	name    string
	summary string
	run     func(args []string, stdout, stderr io.Writer) int
}

// commands is the router table, in the order usage prints them.
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

// Run executes the subcommand named in args and returns the exit code.
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
