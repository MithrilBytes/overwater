// Command regold rewrites goldens/*.md from the fixtures. A golden is
// the byte for byte spec for the renderer and the detectors, and a code
// change that moves one is a bug until a person says otherwise. A price
// change is different: it moves dollar figures and the catalog date in
// the header without any detector being wrong, so the price path runs
// this after applying drift and commits the result with it.
//
//	go run ./tools/regold
package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/MithrilBytes/overwater/catalog"
	"github.com/MithrilBytes/overwater/internal/render"
	"github.com/MithrilBytes/overwater/internal/scan"
	"github.com/MithrilBytes/overwater/rules"
)

// Fixtures are the six the golden test walks, in its order.
var fixtures = []string{
	"ts-chat-firehose", "py-extraction", "node-cron-summarizer",
	"rag-frontier-embeddings", "py-agent-pipeline", "clean-app",
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "regold: %v\n", err)
		os.Exit(2)
	}
}

func run() error {
	// From the source tree, not the embedded snapshot: the price path
	// has just rewritten catalog/ and the binary has not been rebuilt.
	cat, err := catalog.LoadDir("catalog")
	if err != nil {
		return err
	}
	engine, err := rules.Load()
	if err != nil {
		return err
	}
	meta := render.Meta{CatalogVersion: cat.Version, CallsPerMonth: engine.Est.Volume.CallsPerMonth}
	for _, name := range fixtures {
		report, err := scan.Analyze(filepath.Join("fixtures", name), cat)
		if err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		out := render.ModelsMD(engine.Evaluate(report, cat), meta)
		path := filepath.Join("goldens", name+".md")
		old, _ := os.ReadFile(path)
		if string(old) == string(out) {
			continue
		}
		if err := os.WriteFile(path, out, 0o644); err != nil {
			return err
		}
		fmt.Printf("updated %s\n", path)
	}
	return nil
}
