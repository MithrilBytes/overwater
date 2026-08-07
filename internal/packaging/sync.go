package packaging

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Sync writes every manifest under dir for the given release and
// rewrites flake.nix's version. It returns the repository relative
// paths whose bytes changed, so a second run over the same release
// reports nothing.
func Sync(dir, version string, sums map[string]string) ([]string, error) {
	files, err := Render(version, sums)
	if err != nil {
		return nil, err
	}
	flakeSrc, err := os.ReadFile(filepath.Join(dir, FlakePath))
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", FlakePath, err)
	}
	bumped, err := BumpFlake(string(flakeSrc), version)
	if err != nil {
		return nil, err
	}
	files[FlakePath] = bumped

	var changed []string
	for _, name := range sortedKeys(files) {
		full := filepath.Join(dir, filepath.FromSlash(name))
		if old, err := os.ReadFile(full); err == nil && string(old) == files[name] {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			return nil, fmt.Errorf("creating %s: %w", filepath.Dir(name), err)
		}
		if err := os.WriteFile(full, []byte(files[name]), 0o644); err != nil {
			return nil, fmt.Errorf("writing %s: %w", name, err)
		}
		changed = append(changed, name)
	}
	return changed, nil
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

const usage = `Usage: sync-manifests -version vX.Y.Z -sums PATH [-dir REPO]

Rewrites the Homebrew, scoop, and winget manifests and flake.nix from a
release's SHA256SUMS. Pass "-" as PATH to read SHA256SUMS from stdin.
`

// Run is the sync-manifests entry point. It is maintainer tooling: the
// released binary never packages itself.
func Run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("sync-manifests", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() { fmt.Fprint(stderr, usage) }
	version := fs.String("version", "", "release tag, for example v2.1.0")
	sumsPath := fs.String("sums", "", `path to the release's SHA256SUMS, or "-" for stdin`)
	dir := fs.String("dir", ".", "repository root to write into")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *version == "" || *sumsPath == "" {
		fmt.Fprint(stderr, usage)
		return 2
	}
	if rest := fs.Args(); len(rest) > 0 {
		fmt.Fprintf(stderr, "sync-manifests: unexpected argument %q\n", rest[0])
		fmt.Fprint(stderr, usage)
		return 2
	}

	src := stdin
	if *sumsPath != "-" {
		f, err := os.Open(*sumsPath)
		if err != nil {
			fmt.Fprintf(stderr, "sync-manifests: %v\n", err)
			return 2
		}
		defer f.Close()
		src = f
	}
	sums, err := ParseSums(src)
	if err != nil {
		fmt.Fprintf(stderr, "sync-manifests: %v\n", err)
		return 2
	}
	changed, err := Sync(*dir, *version, sums)
	if err != nil {
		fmt.Fprintf(stderr, "sync-manifests: %v\n", err)
		return 2
	}
	if len(changed) == 0 {
		fmt.Fprintf(stdout, "manifests already describe %s\n", *version)
		return 0
	}
	fmt.Fprintf(stdout, "updated %d files for %s:\n  %s\n", len(changed), *version, strings.Join(changed, "\n  "))
	return 0
}
