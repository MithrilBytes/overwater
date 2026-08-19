package packaging

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// Two records of a version live in this tree and they are pinned at
// different moments, which is the whole point of the ordering.
//
// action.yml names the release being cut. It is bumped and committed
// before the tag, because the Action verifies build provenance rather
// than a digest and a version is the one thing knowable in advance.
//
// The packaging manifests name the last release that actually built,
// because Homebrew, scoop and winget need digests and a digest cannot
// exist until the artifacts do. They are pinned by the job that runs
// after the release.
//
// So between a bump and its release the two disagree by exactly one
// version, and that is correct. What is never correct is action.yml
// falling behind the manifests, which would mean a tag shipping an
// older release than the packages do.

var (
	actionVersionRe   = regexp.MustCompile(`version="(v[0-9]+\.[0-9]+\.[0-9]+)"`)
	manifestVersionRe = regexp.MustCompile(`releases/download/(v[0-9]+\.[0-9]+\.[0-9]+)/`)
)

func repoFile(t *testing.T, path string) string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("..", "..", filepath.FromSlash(path)))
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

// assetDigestRe pulls (asset, digest) out of any manifest that names a
// release URL and the checksum for it. Homebrew writes them on adjacent
// lines, scoop and winget as neighbouring fields, so one pattern with a
// bounded gap covers all three.
var assetDigestRe = regexp.MustCompile(
	`(?is)releases/download/v[^/\s"']+/(overwater_[a-z0-9_]+(?:\.exe)?).{0,200}?([0-9a-fA-F]{64})`)

// pinnedRelease reads which release the tree is pinned to. The version
// comes from action.yml, which is the one file that names a release
// without naming a digest. The digests come from the packaging
// manifests, which is where they now live: action.yml stopped carrying
// them so that a tag could carry its own pin, and no single manifest
// covers every platform, so the union of them has to.
// pinnedRelease is the release the packaging manifests describe: the
// version they all name, and the digest each asset is pinned to. No
// single manifest covers every platform, so the union of them has to.
func pinnedRelease(t *testing.T) (string, map[string]string) {
	t.Helper()
	sums := make(map[string]string)
	version := ""
	for _, path := range []string{FormulaPath, ScoopPath, WingetInstallPath} {
		src := repoFile(t, path)
		v := manifestVersionRe.FindStringSubmatch(src)
		if v == nil {
			t.Fatalf("%s names no release download URL, so nothing says which release it pins", path)
		}
		if version == "" {
			version = v[1]
		} else if v[1] != version {
			t.Fatalf("%s pins %s while an earlier manifest pins %s", path, v[1], version)
		}
		for _, m := range assetDigestRe.FindAllStringSubmatch(src, -1) {
			sums[m[1]] = strings.ToLower(m[2])
		}
	}
	for _, asset := range Assets {
		if sums[asset] == "" {
			t.Fatalf("no manifest in the tree pins %s, so nothing verifies that platform", asset)
		}
	}
	return version, sums
}

// The ordering itself. action.yml is bumped first and the manifests
// follow after the release builds, so action.yml may be one version
// ahead and must never be behind.
func TestActionIsNotBehindTheManifests(t *testing.T) {
	manifests, _ := pinnedRelease(t)
	v := actionVersionRe.FindStringSubmatch(repoFile(t, ActionPath))
	if v == nil {
		t.Fatal(`action.yml has no version="vX.Y.Z"; it is the record of which release it fetches`)
	}
	if semverLess(v[1], manifests) {
		t.Errorf("action.yml fetches %s while the packages ship %s, so the Action is the stale one",
			v[1], manifests)
	}
}

// semverLess compares two vX.Y.Z strings numerically, so v2.10.0 sorts
// above v2.9.0 rather than below it.
func semverLess(a, b string) bool {
	pa, pb := parseSemver(a), parseSemver(b)
	for i := range pa {
		if pa[i] != pb[i] {
			return pa[i] < pb[i]
		}
	}
	return false
}

func parseSemver(v string) [3]int {
	var out [3]int
	for i, part := range strings.SplitN(strings.TrimPrefix(v, "v"), ".", 3) {
		n, _ := strconv.Atoi(part)
		out[i] = n
	}
	return out
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

func TestFlakePinsSameRelease(t *testing.T) {
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

// Nix is not a build dependency here, so these are the checks that hold
// without it: the flake declares the outputs a user invokes, stamps the
// symbol the release workflow stamps, pins nixpkgs to a release branch
// rather than a rolling one, and has balanced braces.
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
