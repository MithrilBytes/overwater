package packaging

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// action.yml is the repository's record of which release is pinned: it
// already names a version and a sha256 per platform. The manifests
// describe the same release, so they are checked against it. A bump
// that touches one and not the other fails here rather than in a user's
// package manager.

var (
	actionVersionRe = regexp.MustCompile(`version="(v[0-9]+\.[0-9]+\.[0-9]+)"`)
	actionPinRe     = regexp.MustCompile(`bin="([^"]+)"\s+sha="([0-9a-f]{64})"`)
)

func repoFile(t *testing.T, path string) string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("..", "..", filepath.FromSlash(path)))
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

func pinnedRelease(t *testing.T) (string, map[string]string) {
	t.Helper()
	action := repoFile(t, "action.yml")
	v := actionVersionRe.FindStringSubmatch(action)
	if v == nil {
		t.Fatal(`action.yml has no version="vX.Y.Z"; it is the record of which release is pinned`)
	}
	sums := make(map[string]string)
	for _, m := range actionPinRe.FindAllStringSubmatch(action, -1) {
		sums[m[1]] = m[2]
	}
	if len(sums) != len(Assets) {
		t.Fatalf("action.yml pins %d binaries, the manifests cover %d", len(sums), len(Assets))
	}
	return v[1], sums
}

// The manifests in the tree must be exactly what sync-manifests emits
// for the pinned release: same version, same checksums, no hand edits.
func TestManifestsMatchPinnedRelease(t *testing.T) {
	version, sums := pinnedRelease(t)
	files, err := Render(version, sums)
	if err != nil {
		t.Fatal(err)
	}
	for path, want := range files {
		if got := repoFile(t, path); got != want {
			t.Errorf("%s is stale or hand edited; run\n"+
				"  go run ./tools/sync-manifests -version %s -sums SHA256SUMS\n"+
				"got:\n%s\nwant:\n%s", path, version, got, want)
		}
	}
}

func TestFlakePinsTheSameRelease(t *testing.T) {
	version, _ := pinnedRelease(t)
	flake := repoFile(t, FlakePath)
	want := `version = "` + strings.TrimPrefix(version, "v") + `";`
	if !strings.Contains(flake, want) {
		t.Errorf("flake.nix does not carry %s; want a line with %q", version, want)
	}
	if strings.Count(flake, "version = \"") != 1 {
		t.Error("flake.nix has more than one version assignment; sync-manifests rewrites every one")
	}
}

// Nix is not a build dependency of this repository and is not installed
// on every machine, so these are the checks that hold without it: the
// flake declares the outputs a user invokes, stamps the same symbol the
// release workflow stamps, pins nixpkgs to a release branch rather than
// a rolling one, and has balanced braces.
func TestFlakeStructure(t *testing.T) {
	flake := repoFile(t, FlakePath)
	for _, want := range []string{
		"packages = ",
		"apps = ",
		"checks = ",
		"default = overwater;",
		`type = "app";`,
		"buildGoModule",
		"vendorHash = ",
		`subPackages = [ "cmd/overwater" ];`,
		"-X github.com/MithrilBytes/overwater/internal/cli.buildVersion=v${version}",
	} {
		if !strings.Contains(flake, want) {
			t.Errorf("flake.nix is missing %q", want)
		}
	}

	input := regexp.MustCompile(`nixpkgs\.url = "github:NixOS/nixpkgs/([^"]+)"`).FindStringSubmatch(flake)
	if input == nil {
		t.Fatal("flake.nix does not take nixpkgs from github:NixOS/nixpkgs")
	}
	pinned := regexp.MustCompile(`^(nixos-[0-9]{2}\.[0-9]{2}|[0-9a-f]{40})$`)
	if !pinned.MatchString(input[1]) {
		t.Errorf("nixpkgs is on %q; pin a nixos-YY.MM branch or a revision", input[1])
	}

	if depth, line := braceBalance(flake); depth != 0 {
		t.Errorf("flake.nix braces do not balance: depth %d after line %d", depth, line)
	}
}

// braceBalance counts braces outside comments and string literals and
// returns the final depth, plus the line where it first went negative.
func braceBalance(src string) (int, int) {
	depth, line, bad := 0, 1, 0
	inString, inComment := false, false
	for i := 0; i < len(src); i++ {
		c := src[i]
		switch {
		case c == '\n':
			line++
			inComment = false
		case inComment:
		case inString:
			if c == '\\' {
				i++
			} else if c == '"' {
				inString = false
			}
		case c == '"':
			inString = true
		case c == '#':
			inComment = true
		case c == '{':
			depth++
		case c == '}':
			depth--
			if depth < 0 && bad == 0 {
				bad = line
			}
		}
	}
	if depth != 0 && bad == 0 {
		bad = line
	}
	return depth, bad
}

func TestBraceBalance(t *testing.T) {
	cases := map[string]int{
		"{ }":                 0,
		"{ ":                  1,
		"} }":                 -2,
		`{ x = "}"; }`:        0,
		"{ # }\n}":            0,
		`{ x = "\""; }`:       0,
		"{ y = ''; }":         0,
		"{ a = { b = 1; }; }": 0,
	}
	for src, want := range cases {
		if got, _ := braceBalance(src); got != want {
			t.Errorf("%q: got depth %d, want %d", src, got, want)
		}
	}
}
