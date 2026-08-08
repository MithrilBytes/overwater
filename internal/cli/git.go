package cli

import (
	"fmt"
	"io"
	"os/exec"
	"strings"

	"github.com/MithrilBytes/overwater/internal/baseline"
)

// gitHead returns the commit sha of the repository containing root, or
// "" when git or a repository is absent. Baselines record it for
// --incremental to diff against. Local git only, no network.
func gitHead(root string) string {
	if root == "" {
		return ""
	}
	out, err := exec.Command("git", "-C", root, "rev-parse", "HEAD").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// gitChangedFiles lists the files changed since sha plus untracked
// files, relative to root. Any git failure is returned so the caller
// can fall back to a full scan.
//
// -z is required: under the default core.quotePath git octal escapes
// any path with a non-ASCII byte, and that string matches no real file.
func gitChangedFiles(root, sha string) (map[string]bool, error) {
	diff, err := exec.Command("git", "-C", root, "diff", "--relative", "--name-only", "-z", sha).Output()
	if err != nil {
		return nil, fmt.Errorf("git diff: %w", err)
	}
	untracked, err := exec.Command("git", "-C", root, "ls-files", "--others", "--exclude-standard", "-z").Output()
	if err != nil {
		return nil, fmt.Errorf("git ls-files: %w", err)
	}
	only := map[string]bool{}
	// Paths may begin or end with a space, so do not trim; a NUL split
	// only needs its empty final record dropped.
	for _, name := range strings.Split(string(diff)+string(untracked), "\x00") {
		if name != "" {
			only[name] = true
		}
	}
	return only, nil
}

// incrementalSet resolves --incremental into the set of files to scan.
// A nil result means full scan; the reason is already on stderr.
func incrementalSet(root, baselinePath string, stderr io.Writer) map[string]bool {
	bl, err := baseline.Load(baselinePath)
	if err != nil {
		fmt.Fprintf(stderr, "incremental: %v; scanning everything\n", err)
		return nil
	}
	if bl.Commit == "" {
		fmt.Fprintln(stderr, "incremental: baseline has no commit recorded; scanning everything")
		return nil
	}
	only, err := gitChangedFiles(root, bl.Commit)
	if err != nil {
		fmt.Fprintf(stderr, "incremental: %v; scanning everything\n", err)
		return nil
	}
	return only
}
