package packaging

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// The two ends of the release path that no Go code covers. SHA256SUMS
// is the file every manifest, the Action and the flake are pinned from,
// so the workflow has to hand the pin job the bytes it built rather
// than whatever the published release holds by then. install.sh is the
// documented curl path, where the same sums certify the binary they
// shipped beside and prove only that it arrived intact.

const (
	installPath      = "scripts/install.sh"
	releaseFlowPath  = ".github/workflows/release.yml"
	provenanceAction = "actions/attest-build-provenance@"
)

type workflowStep struct {
	Name string         `yaml:"name"`
	Uses string         `yaml:"uses"`
	With map[string]any `yaml:"with"`
	Run  string         `yaml:"run"`
}

func releaseSteps(t *testing.T, job string) []workflowStep {
	t.Helper()
	var doc struct {
		Jobs map[string]struct {
			Steps []workflowStep `yaml:"steps"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal([]byte(repoFile(t, releaseFlowPath)), &doc); err != nil {
		t.Fatalf("%s does not parse: %v", releaseFlowPath, err)
	}
	j, ok := doc.Jobs[job]
	if !ok {
		t.Fatalf("%s has no %s job", releaseFlowPath, job)
	}
	if len(j.Steps) == 0 {
		t.Fatalf("%s job %s has no steps", releaseFlowPath, job)
	}
	return j.Steps
}

// dist/overwater_* does not match SHA256SUMS, so the one file the whole
// distribution is pinned from shipped with no provenance at all.
func TestReleaseAttestsTheSums(t *testing.T) {
	for _, s := range releaseSteps(t, "release") {
		if !strings.HasPrefix(s.Uses, provenanceAction) {
			continue
		}
		subjects := strings.Fields(fmt.Sprint(s.With["subject-path"]))
		for _, p := range subjects {
			if p == "dist/SHA256SUMS" {
				return
			}
		}
		t.Fatalf("attestation subjects %v do not cover dist/SHA256SUMS", subjects)
	}
	t.Fatal("the release job does not attest build provenance")
}

// The pin job commits these checksums to main, where the Action,
// Homebrew, scoop, winget and the flake then enforce them. Fetching
// them back out of the release puts a mutable public surface between
// the job that built them and the job that trusts them.
func TestPinTakesTheSumsFromTheBuild(t *testing.T) {
	var uploads bool
	for _, s := range releaseSteps(t, "release") {
		if strings.HasPrefix(s.Uses, "actions/upload-artifact@") {
			uploads = true
		}
	}
	if !uploads {
		t.Error("the release job never hands the sums it built to the pin job")
	}

	var downloads bool
	for _, s := range releaseSteps(t, "pin") {
		if strings.HasPrefix(s.Uses, "actions/download-artifact@") {
			downloads = true
		}
		if strings.Contains(s.Run, "gh release download") {
			t.Errorf("step %q reads the sums back off the release:\n%s", s.Name, s.Run)
		}
	}
	if !downloads {
		t.Error("the pin job never takes the sums the release job built")
	}
}

// installBinary stands in for the release binary. It is a script so the
// version line install.sh runs at the end still works.
const installBinary = "#!/bin/sh\necho overwater v9.9.9\n"

// curlStub serves $RELEASE_DIR in place of the release download URL.
const curlStub = `#!/bin/sh
out=""
url=""
while [ $# -gt 0 ]; do
  case "$1" in
    -o) out="$2"; shift 2 ;;
    -*) shift ;;
    *) url="$1"; shift ;;
  esac
done
name="${url##*/}"
if [ ! -f "$RELEASE_DIR/$name" ]; then
  echo "curl: (22) $url not found" >&2
  exit 22
fi
cat "$RELEASE_DIR/$name" > "$out"
`

func installAsset(t *testing.T) string {
	t.Helper()
	switch runtime.GOOS + "/" + runtime.GOARCH {
	case "darwin/amd64", "darwin/arm64", "linux/amd64", "linux/arm64":
	default:
		t.Skipf("install.sh serves no %s/%s binary", runtime.GOOS, runtime.GOARCH)
	}
	return "overwater_" + runtime.GOOS + "_" + runtime.GOARCH
}

func installDigest() string {
	sum := sha256.Sum256([]byte(installBinary))
	return hex.EncodeToString(sum[:])
}

func writeFile(t *testing.T, path, body string, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), mode); err != nil {
		t.Fatal(err)
	}
}

// stubBin is the only PATH install.sh gets: a gh installed on the
// machine running the tests would otherwise decide which branch the
// no gh case takes.
func stubBin(t *testing.T, dir string) string {
	t.Helper()
	bin := filepath.Join(dir, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, tool := range []string{"uname", "mktemp", "grep", "cat", "cp", "rm", "mkdir", "install", "tr", "sha256sum", "shasum"} {
		p, err := exec.LookPath(tool)
		if err != nil {
			continue
		}
		if err := os.Symlink(p, filepath.Join(bin, tool)); err != nil {
			t.Fatal(err)
		}
	}
	return bin
}

type installCase struct {
	sums string   // what the fake release serves as SHA256SUMS; none if empty
	gh   string   // shell appended to the gh stub; gh is off PATH if empty
	env  []string // extra environment for install.sh
}

type installResult struct {
	code      int
	stderr    string
	installed bool
	ghArgs    []string
}

func runInstall(t *testing.T, tc installCase) installResult {
	t.Helper()
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("sh is not on PATH")
	}
	asset := installAsset(t)
	dir := t.TempDir()

	release := filepath.Join(dir, "release")
	if err := os.MkdirAll(release, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(release, asset), installBinary, 0o755)
	if tc.sums != "" {
		writeFile(t, filepath.Join(release, "SHA256SUMS"), tc.sums, 0o644)
	}

	bin := stubBin(t, dir)
	writeFile(t, filepath.Join(bin, "curl"), curlStub, 0o755)
	ghArgs := filepath.Join(dir, "gh-args")
	if tc.gh != "" {
		writeFile(t, filepath.Join(bin, "gh"),
			"#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$GH_ARGS\"\n"+tc.gh+"\n", 0o755)
	}

	target := filepath.Join(dir, "target")
	cmd := exec.Command(sh, filepath.Join("..", "..", filepath.FromSlash(installPath)), "v9.9.9", target)
	cmd.Env = append([]string{
		"PATH=" + bin,
		"HOME=" + dir,
		"RELEASE_DIR=" + release,
		"GH_ARGS=" + ghArgs,
	}, tc.env...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr

	res := installResult{}
	var exit *exec.ExitError
	switch err := cmd.Run(); {
	case err == nil:
	case errors.As(err, &exit):
		res.code = exit.ExitCode()
	default:
		t.Fatalf("running %s: %v", installPath, err)
	}
	res.stderr = stderr.String()
	if _, err := os.Stat(filepath.Join(target, "overwater")); err == nil {
		res.installed = true
	}
	if raw, err := os.ReadFile(ghArgs); err == nil {
		res.ghArgs = strings.Fields(string(raw))
	}
	return res
}

func sumsFor(t *testing.T, sep, digest string) string {
	t.Helper()
	return digest + sep + installAsset(t) + "\n"
}

// A release whose SHA256SUMS was written in binary mode is a release
// nobody can install: the name is preceded by an asterisk, and the
// checksum line is looked up by name. ParseSums has always tolerated it.
func TestInstallAcceptsBinaryModeSums(t *testing.T) {
	res := runInstall(t, installCase{
		sums: sumsFor(t, " *", installDigest()),
		gh:   "exit 0",
	})
	if res.code != 0 || !res.installed {
		t.Fatalf("exit %d, installed %v; stderr: %s", res.code, res.installed, res.stderr)
	}
}

// The provenance is the only check here that a replaced release asset
// does not also get to replace, so failing it stops the install.
func TestInstallStopsWhenProvenanceFails(t *testing.T) {
	res := runInstall(t, installCase{
		sums: sumsFor(t, "  ", installDigest()),
		gh:   "echo 'no attestation matches' >&2\nexit 1",
	})
	if res.code != 2 {
		t.Errorf("exit %d, want 2; stderr: %s", res.code, res.stderr)
	}
	if res.installed {
		t.Error("the binary was installed after its provenance failed")
	}
	if !strings.Contains(res.stderr, "provenance") {
		t.Errorf("stderr does not say why: %s", res.stderr)
	}
}

// Verifying the wrong file, or against the wrong repository, verifies
// nothing.
func TestInstallVerifiesTheDownloadedBinary(t *testing.T) {
	res := runInstall(t, installCase{
		sums: sumsFor(t, "  ", installDigest()),
		gh:   "exit 0",
	})
	if res.code != 0 {
		t.Fatalf("exit %d; stderr: %s", res.code, res.stderr)
	}
	joined := strings.Join(res.ghArgs, " ")
	if !strings.HasPrefix(joined, "attestation verify ") {
		t.Fatalf("gh was called as %q", joined)
	}
	if !strings.HasSuffix(joined, " --repo MithrilBytes/overwater") {
		t.Errorf("gh was not pointed at this repository: %q", joined)
	}
	if !strings.HasSuffix(res.ghArgs[2], "/"+installAsset(t)) {
		t.Errorf("gh verified %q, not the downloaded binary", res.ghArgs[2])
	}
}

// A digest the caller got somewhere other than this release is the only
// one worth checking when gh is unavailable, so it wins over SHA256SUMS
// and is enough on its own.
func TestInstallTakesADigestOutOfBand(t *testing.T) {
	res := runInstall(t, installCase{
		env: []string{"OVERWATER_SHA256=" + strings.ToUpper(installDigest())},
	})
	if res.code != 0 || !res.installed {
		t.Fatalf("exit %d, installed %v; stderr: %s", res.code, res.installed, res.stderr)
	}

	// SHA256SUMS agreeing with the binary does not overrule it.
	res = runInstall(t, installCase{
		sums: sumsFor(t, "  ", installDigest()),
		env:  []string{"OVERWATER_SHA256=" + strings.Repeat("a", 64)},
	})
	if res.code != 2 {
		t.Errorf("exit %d, want 2; stderr: %s", res.code, res.stderr)
	}
	if res.installed {
		t.Error("a binary that did not match the supplied digest was installed")
	}
}

// Without gh nothing here proves the binary is the one the release
// built, and the install says so rather than implying otherwise.
func TestInstallSaysWhatTheSumsAloneProve(t *testing.T) {
	res := runInstall(t, installCase{sums: sumsFor(t, "  ", installDigest())})
	if res.code != 0 || !res.installed {
		t.Fatalf("exit %d, installed %v; stderr: %s", res.code, res.installed, res.stderr)
	}
	if !strings.Contains(res.stderr, "gh") {
		t.Errorf("nothing warned that the provenance went unchecked: %q", res.stderr)
	}
}

// A tampered binary is an operational failure, not a finding.
func TestInstallRejectsATamperedBinary(t *testing.T) {
	res := runInstall(t, installCase{
		sums: sumsFor(t, "  ", strings.Repeat("b", 64)),
		gh:   "exit 0",
	})
	if res.code != 2 {
		t.Errorf("exit %d, want 2; stderr: %s", res.code, res.stderr)
	}
	if res.installed {
		t.Error("a binary that did not match SHA256SUMS was installed")
	}
}
