// Command relnotes turns git log subjects on stdin into release notes on
// stdout. The release workflow is its only caller:
//
//	git log --no-merges --format=%s v1..v2 |
//	  go run ./internal/release/cmd -prev v1 -tag v2 -repo owner/name
package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/MithrilBytes/overwater/internal/release"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("relnotes", flag.ContinueOnError)
	fs.SetOutput(stderr)
	prev := fs.String("prev", "", "previous tag, empty for the first release")
	tag := fs.String("tag", "", "tag being released")
	repo := fs.String("repo", "", "owner/name slug for the commit link")
	nextTag := fs.Bool("next-tag", false, "read tags on stdin, print the next tag, and exit")
	bump := fs.String("bump", "patch", "with -next-tag: patch, minor or major")
	classify := fs.Bool("classify", false, "read commit subjects on stdin, print the release they add up to (major, minor, patch, or nothing), and exit")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *classify {
		subjects, err := lines(stdin)
		if err != nil {
			fmt.Fprintf(stderr, "relnotes: reading subjects: %v\n", err)
			return 2
		}
		fmt.Fprintln(stdout, release.BumpFor(subjects))
		return 0
	}
	if *nextTag {
		tags, err := lines(stdin)
		if err != nil {
			fmt.Fprintf(stderr, "relnotes: reading tags: %v\n", err)
			return 2
		}
		next, err := release.NextTag(tags, *bump)
		if err != nil {
			fmt.Fprintf(stderr, "relnotes: %v\n", err)
			return 2
		}
		fmt.Fprintln(stdout, next)
		return 0
	}
	if *tag == "" || *repo == "" {
		fmt.Fprintln(stderr, "relnotes: -tag and -repo are required")
		return 2
	}

	subjects, err := lines(stdin)
	if err != nil {
		fmt.Fprintf(stderr, "relnotes: read commit subjects from stdin: %v\n", err)
		return 2
	}
	fmt.Fprint(stdout, release.Notes(subjects, *prev, *tag, *repo))
	return 0
}

// lines reads r line by line. The buffer is wide enough for a commit
// subject nobody trimmed.
func lines(r io.Reader) ([]string, error) {
	var out []string
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64<<10), 1<<20)
	for sc.Scan() {
		out = append(out, sc.Text())
	}
	return out, sc.Err()
}
