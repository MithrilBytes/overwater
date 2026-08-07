// Command sync-manifests rewrites the package manager manifests and
// flake.nix from a release's SHA256SUMS. It is maintainer tooling and
// ships with the source, not with the release binaries.
package main

import (
	"os"

	"github.com/MithrilBytes/overwater/internal/packaging"
)

func main() {
	os.Exit(packaging.Run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}
