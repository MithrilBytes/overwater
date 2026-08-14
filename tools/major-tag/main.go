// Command major-tag prints the floating major tag for a release, so the
// release workflow does not have to parse a version in shell.
package main

import (
	"fmt"
	"os"

	"github.com/MithrilBytes/overwater/internal/packaging"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: major-tag vX.Y.Z")
		os.Exit(2)
	}
	tag, err := packaging.MajorTag(os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, "major-tag:", err)
		os.Exit(2)
	}
	fmt.Println(tag)
}
