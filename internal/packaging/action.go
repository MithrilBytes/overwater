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

var pinVersionRe = regexp.MustCompile(`(?m)^(\s*version=")v?[0-9][^"]*(")`)

// PinAction rewrites action.yml's pinned version. It used to write per
// asset checksums too, which is what made a tag unable to carry its own
// pin: a digest exists only after the artifacts are built, so it could
// only be committed after the tag already pointed somewhere. The Action
// verifies build provenance instead, and a version is knowable before
// the tag, so this can now run before the release rather than after it.
func PinAction(src, version string) (string, error) {
	v, err := normalizeVersion(version)
	if err != nil {
		return "", err
	}
	if !pinVersionRe.MatchString(src) {
		return "", fmt.Errorf("%s has no version= line to pin", ActionPath)
	}
	return pinVersionRe.ReplaceAllString(src, "${1}v"+v+"${2}"), nil
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
