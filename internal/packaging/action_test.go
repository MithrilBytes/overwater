package packaging

import (
	"strings"
	"testing"
)

func actionFixture() string {
	return `name: overwater
runs:
  using: composite
  steps:
    - name: fetch pinned overwater binary
      shell: bash
      run: |
        version="v1.0.0"
        repo="MithrilBytes/overwater"
        case "${RUNNER_OS}_${RUNNER_ARCH}" in
          Linux_X64)     bin="overwater_linux_amd64"       ;;
          Windows_X64)   bin="overwater_windows_amd64.exe" ;;
        esac
`
}

func fullSums() map[string]string {
	return map[string]string{
		"overwater_linux_amd64":       strings.Repeat("a", 64),
		"overwater_linux_arm64":       strings.Repeat("b", 64),
		"overwater_darwin_amd64":      strings.Repeat("c", 64),
		"overwater_darwin_arm64":      strings.Repeat("d", 64),
		"overwater_windows_amd64.exe": strings.Repeat("e", 64),
	}
}

func TestPinActionRewritesTheVersion(t *testing.T) {
	out, err := PinAction(actionFixture(), "v2.4.0")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `version="v2.4.0"`) {
		t.Errorf("version not pinned:\n%s", out)
	}
	if strings.Contains(out, `version="v1.0.0"`) {
		t.Error("the old version survived the pin")
	}
}

func TestPinActionRejectsBadInput(t *testing.T) {
	for _, v := range []string{"", "2.4", "latest", "v2.4.0.1"} {
		if _, err := PinAction(actionFixture(), v); err == nil {
			t.Errorf("version %q was accepted", v)
		}
	}
	if _, err := PinAction("name: overwater\n", "v2.4.0"); err == nil {
		t.Error("a file with no version line was accepted")
	}
}

func TestMajorTag(t *testing.T) {
	for version, want := range map[string]string{
		"v2.4.0": "v2", "2.4.1": "v2", "v10.0.0": "v10", "v3.11.9": "v3",
	} {
		got, err := MajorTag(version)
		if err != nil {
			t.Fatalf("MajorTag(%q): %v", version, err)
		}
		if got != want {
			t.Errorf("MajorTag(%q) = %q, want %q", version, got, want)
		}
	}
	if _, err := MajorTag("latest"); err == nil {
		t.Error("MajorTag accepted a non version")
	}
}

// The repository's own action.yml has to be pinnable, or the automation
// that runs before every release fails on the file it exists to update.
func TestRepoActionIsPinnable(t *testing.T) {
	out, err := PinAction(repoFile(t, ActionPath), "v9.9.9")
	if err != nil {
		t.Fatalf("the committed action.yml cannot be pinned: %v", err)
	}
	if !strings.Contains(out, `version="v9.9.9"`) {
		t.Error("version was not rewritten")
	}
}

// The digests are gone on purpose: a checksum cannot exist until the
// artifacts are built, so committing one meant committing after the tag,
// which is exactly how vX.Y.Z came to fetch the previous release. If a
// sha= line reappears here, that ordering problem has come back with it.
func TestRepoActionCarriesNoCommittedDigest(t *testing.T) {
	src := repoFile(t, ActionPath)
	if strings.Contains(src, `sha="`) {
		t.Error("action.yml pins a digest again, which a tag cannot carry for its own release")
	}
	if !strings.Contains(src, "gh attestation verify") {
		t.Error("action.yml no longer verifies build provenance, so nothing checks the binary's origin")
	}
}
