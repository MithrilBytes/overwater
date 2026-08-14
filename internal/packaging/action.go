package packaging

import (
	"fmt"
	"regexp"
	"strings"
)

// Pinning the composite Action to a release.
//
// action.yml downloads a release binary and checks it against a sha256
// written into the file. The checksums cannot exist until that release
// has built, so a tag can never carry its own pin: whatever action.yml
// says at v2.4.0 was true of v2.3.0. That was worked around by hand for
// three releases, badly, since the README told readers to use a tag one
// behind and the price release path repinned nothing at all.
//
// The pin is written after the release exists, by the workflow that
// built it, and the floating major tag is moved to that commit. A
// reader who follows the README to `@v2` gets the pin for the release
// that is actually current.

const ActionPath = "action.yml"

var (
	pinVersionRe = regexp.MustCompile(`(?m)^(\s*version=")v?[0-9][^"]*(")`)
	// Each platform line names its asset and its checksum. Keyed on the
	// asset so a reordered case statement still pins correctly.
	pinShaRe = regexp.MustCompile(`bin="([A-Za-z0-9_.-]+)"(\s+)sha="[0-9a-fA-F]{64}"`)
)

// PinAction rewrites action.yml's pinned version and per asset
// checksums. It reports an error rather than a partial pin when the
// release is missing an asset the Action knows how to download, since a
// half pinned Action fails only on the platform nobody tested.
func PinAction(src, version string, sums map[string]string) (string, error) {
	v, err := normalizeVersion(version)
	if err != nil {
		return "", err
	}
	if !pinVersionRe.MatchString(src) {
		return "", fmt.Errorf("%s has no version= line to pin", ActionPath)
	}
	out := pinVersionRe.ReplaceAllString(src, "${1}v"+v+"${2}")

	var missing []string
	out = pinShaRe.ReplaceAllStringFunc(out, func(m string) string {
		parts := pinShaRe.FindStringSubmatch(m)
		asset, gap := parts[1], parts[2]
		sum, ok := sums[asset]
		if !ok {
			missing = append(missing, asset)
			return m
		}
		return fmt.Sprintf("bin=%q%ssha=%q", asset, gap, sum)
	})
	if len(missing) > 0 {
		return "", fmt.Errorf("%s pins assets the release does not carry: %s",
			ActionPath, strings.Join(missing, ", "))
	}
	if !strings.Contains(out, "sha=\"") {
		return "", fmt.Errorf("%s has no sha= lines to pin", ActionPath)
	}
	return out, nil
}

// MajorTag is the floating tag for a release: v2.4.1 floats at v2. It
// is what the README tells readers to use, so it has to move to every
// pin commit.
func MajorTag(version string) (string, error) {
	v, err := normalizeVersion(version)
	if err != nil {
		return "", err
	}
	return "v" + v[:strings.Index(v, ".")], nil
}

func normalizeVersion(version string) (string, error) {
	m := versionRe.FindStringSubmatch(strings.TrimSpace(version))
	if m == nil {
		return "", fmt.Errorf("version %q is not vX.Y.Z; pass the release tag, for example v2.1.0", version)
	}
	return m[1], nil
}
