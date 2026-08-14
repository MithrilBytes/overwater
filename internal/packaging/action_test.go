package packaging

import (
	"regexp"
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
        case "${RUNNER_OS}_${RUNNER_ARCH}" in
          Linux_X64)     bin="overwater_linux_amd64"       sha="` + strings.Repeat("0", 64) + `" ;;
          Linux_ARM64)   bin="overwater_linux_arm64"       sha="` + strings.Repeat("1", 64) + `" ;;
          macOS_X64)     bin="overwater_darwin_amd64"      sha="` + strings.Repeat("2", 64) + `" ;;
          macOS_ARM64)   bin="overwater_darwin_arm64"      sha="` + strings.Repeat("3", 64) + `" ;;
          Windows_X64)   bin="overwater_windows_amd64.exe" sha="` + strings.Repeat("4", 64) + `" ;;
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

func TestPinActionRewritesVersionAndEverySum(t *testing.T) {
	out, err := PinAction(actionFixture(), "v2.4.0", fullSums())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `version="v2.4.0"`) {
		t.Errorf("version not pinned:\n%s", out)
	}
	pairs := regexp.MustCompile(`bin="([^"]+)"\s+sha="([0-9a-f]{64})"`).FindAllStringSubmatch(out, -1)
	if len(pairs) != 5 {
		t.Fatalf("found %d pinned assets, want 5", len(pairs))
	}
	for _, p := range pairs {
		if want := fullSums()[p[1]]; p[2] != want {
			t.Errorf("%s pinned to %s, want %s", p[1], p[2], want)
		}
	}
	// The old sums are the failure this guards: a half rewritten file
	// fails only on the platform nobody tested.
	for _, stale := range []string{strings.Repeat("0", 64), strings.Repeat("4", 64)} {
		if strings.Contains(out, stale) {
			t.Errorf("a stale checksum survived the pin")
		}
	}
}

// A release missing an asset must fail loudly, not pin four of five.
func TestPinActionRefusesAPartialRelease(t *testing.T) {
	sums := fullSums()
	delete(sums, "overwater_windows_amd64.exe")
	_, err := PinAction(actionFixture(), "v2.4.0", sums)
	if err == nil {
		t.Fatal("a release missing an asset was accepted")
	}
	if !strings.Contains(err.Error(), "overwater_windows_amd64.exe") {
		t.Errorf("error does not name the missing asset: %v", err)
	}
}

func TestPinActionRejectsBadInput(t *testing.T) {
	for _, v := range []string{"", "2.4", "latest", "v2.4.0.1"} {
		if _, err := PinAction(actionFixture(), v, fullSums()); err == nil {
			t.Errorf("version %q was accepted", v)
		}
	}
	if _, err := PinAction("name: overwater\n", "v2.4.0", fullSums()); err == nil {
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
// that runs after every release fails on the file it exists to update.
func TestRepoActionIsPinnable(t *testing.T) {
	out, err := PinAction(repoFile(t, ActionPath), "v9.9.9", fullSums())
	if err != nil {
		t.Fatalf("the committed action.yml cannot be pinned: %v", err)
	}
	if !strings.Contains(out, `version="v9.9.9"`) {
		t.Error("version was not rewritten")
	}
	for _, sum := range fullSums() {
		if !strings.Contains(out, sum) {
			t.Errorf("checksum %s... was not written", sum[:8])
		}
	}
}
