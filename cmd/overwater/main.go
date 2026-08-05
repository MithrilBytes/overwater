// Command overwater flags LLM call sites that use more model than the
// task in front of them needs.
package main

import (
	"os"

	"github.com/MithrilBytes/overwater/internal/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:], os.Stdout, os.Stderr))
}
