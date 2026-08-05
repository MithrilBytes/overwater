// Package cli routes overwater's subcommands and owns the exit code
// contract. CI scripts branch on these codes, so findings (1) and
// operational errors (2) are never conflated.
package cli

import (
	"fmt"
	"io"
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

func runScan(_ []string, _, stderr io.Writer) int {
	fmt.Fprintln(stderr, "overwater: scan is not implemented yet")
	return ExitError
}

func runEval(_ []string, _, stderr io.Writer) int {
	fmt.Fprintln(stderr, "overwater: eval is not implemented yet")
	return ExitError
}

func runCatalog(_ []string, _, stderr io.Writer) int {
	fmt.Fprintln(stderr, "overwater: catalog is not implemented yet")
	return ExitError
}
