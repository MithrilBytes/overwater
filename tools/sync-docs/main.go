// Command sync-docs rewrites the facts in README.md and site/index.html
// from the repository itself. Run it after adding a rule or naming a new
// release in action.yml:
//
//	go run ./tools/sync-docs
//
// It prints the files it changed and exits 0 either way. TestDocsAreInSync
// fails when the committed files differ from what this produces, and the
// docs workflow runs it on main so a push that forgets cannot leave the
// pages behind.
package main

import (
	"fmt"
	"os"

	"github.com/MithrilBytes/overwater/internal/docs"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "sync-docs: %v\n", err)
		os.Exit(2)
	}
}

func run() error {
	facts, err := docs.Read(".")
	if err != nil {
		return err
	}
	for _, target := range []struct {
		path  string
		apply func(string, docs.Facts) (string, error)
	}{
		{"README.md", docs.README},
		{"site/index.html", docs.Site},
	} {
		src, err := os.ReadFile(target.path)
		if err != nil {
			return err
		}
		out, err := target.apply(string(src), facts)
		if err != nil {
			return fmt.Errorf("%s: %w", target.path, err)
		}
		if out == string(src) {
			continue
		}
		if err := os.WriteFile(target.path, []byte(out), 0o644); err != nil {
			return err
		}
		fmt.Printf("updated %s\n", target.path)
	}
	return nil
}
