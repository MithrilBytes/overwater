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
	nextTags := fs.Bool("next-tags", false, "read tags on stdin, print the update tag and its semver twin, and exit")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *nextTags {
		var tags []string
		sc := bufio.NewScanner(stdin)
		for sc.Scan() {
			tags = append(tags, sc.Text())
		}
		if err := sc.Err(); err != nil {
			fmt.Fprintf(stderr, "relnotes: reading tags: %v\n", err)
			return 2
		}
		update, twin, err := release.NextTags(tags)
		if err != nil {
			fmt.Fprintf(stderr, "relnotes: %v\n", err)
			return 2
		}
		fmt.Fprintf(stdout, "%s %s\n", update, twin)
		return 0
	}
	if *tag == "" || *repo == "" {
		fmt.Fprintln(stderr, "relnotes: -tag and -repo are required")
		return 2
	}

	var subjects []string
	sc := bufio.NewScanner(stdin)
	sc.Buffer(make([]byte, 0, 64<<10), 1<<20)
	for sc.Scan() {
		subjects = append(subjects, sc.Text())
	}
	if err := sc.Err(); err != nil {
		fmt.Fprintf(stderr, "relnotes: read commit subjects from stdin: %v\n", err)
		return 2
	}
	fmt.Fprint(stdout, release.Notes(subjects, *prev, *tag, *repo))
	return 0
}
