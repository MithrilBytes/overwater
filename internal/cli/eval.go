package cli

import (
	"flag"
	"fmt"
	"io"

	"github.com/MithrilBytes/overwater/catalog"
	"github.com/MithrilBytes/overwater/internal/evalgen"
	"github.com/MithrilBytes/overwater/internal/render"
	"github.com/MithrilBytes/overwater/rules"
)

// analyzeRepo scans one root for eval. A blown budget is a guard
// concern; eval ignores it.
func analyzeRepo(root string, volume int, stderr io.Writer) (*catalog.Catalog, []rules.Finding, error) {
	p, err := newPipeline(volume, nil, stderr)
	if err != nil {
		return nil, nil, err
	}
	pl, err := planRoot(root)
	if err != nil {
		return nil, nil, err
	}
	res, err := p.scanRoot(pl, nil, p.volumeFor(pl, volume))
	if err != nil {
		return nil, nil, err
	}
	return p.cat, res.findings, nil
}

// runEval generates one A/B eval script per finding that nominates a
// different model. The user supplies the prompts and keys and runs the
// scripts; overwater never does.
func runEval(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("eval", flag.ContinueOnError)
	fs.SetOutput(stderr)
	outDir := fs.String("o", "overwater-evals", "directory for generated scripts")
	volume := fs.Int("volume", 0, "estimated calls per month per call site")
	draft := fs.Bool("draft-prompts", false, "seed a prompts.jsonl per script from literals near the call site")
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
	cat, findings, err := analyzeRepo(root, *volume, stderr)
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
	if *draft {
		drafts, err := evalgen.DraftPromptSets(root, findings, *outDir)
		if err != nil {
			fmt.Fprintf(stderr, "overwater: %v\n", err)
			return ExitError
		}
		for _, path := range drafts {
			fmt.Fprintf(stdout, "wrote %s (drafted; real production prompts beat these)\n", path)
		}
	}
	if len(written) == 0 {
		fmt.Fprintln(stdout, "no findings nominate a different model, so there is nothing to eval")
	}
	return ExitClean
}
