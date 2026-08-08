package packaging

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// Hashes in testdata/SHA256SUMS, one repeated byte per platform so a
// checksum landing in the wrong manifest is obvious.
const (
	fixDarwinAMD64  = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	fixDarwinARM64  = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	fixLinuxAMD64   = "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
	fixLinuxARM64   = "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"
	fixWindowsAMD64 = "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
)

func fixtureSums(t *testing.T) map[string]string {
	t.Helper()
	f, err := os.Open(filepath.Join("testdata", "SHA256SUMS"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	sums, err := ParseSums(f)
	if err != nil {
		t.Fatalf("fixture SHA256SUMS does not parse: %v", err)
	}
	return sums
}

func TestParseSums(t *testing.T) {
	sums := fixtureSums(t)
	want := map[string]string{
		assetDarwinAMD64:  fixDarwinAMD64,
		assetDarwinARM64:  fixDarwinARM64,
		assetLinuxAMD64:   fixLinuxAMD64,
		assetLinuxARM64:   fixLinuxARM64,
		assetWindowsAMD64: fixWindowsAMD64,
	}
	for name, hash := range want {
		if sums[name] != hash {
			t.Errorf("%s: got %q, want %q", name, sums[name], hash)
		}
	}
	// Assets the manifests do not name are carried, not rejected.
	if sums["overwater_plan9_amd64"] == "" {
		t.Error("an unreferenced asset was dropped")
	}
}

func TestParseSumsRejects(t *testing.T) {
	cases := map[string]struct{ in, want string }{
		"short hash":  {"abc  overwater_linux_amd64\n", "not a 64 character hex"},
		"no filename": {strings.Repeat("a", 64) + "\n", "<sha256>  <file>"},
		"duplicate": {strings.Repeat("a", 64) + "  overwater_linux_amd64\n" +
			strings.Repeat("b", 64) + "  overwater_linux_amd64\n", "twice"},
		"empty":            {"", "no checksum lines"},
		"comments only":    {"# nothing here\n\n", "no checksum lines"},
		"hash with prefix": {"sha256:" + strings.Repeat("a", 57) + "  overwater_linux_amd64\n", "not a 64 character hex"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := ParseSums(strings.NewReader(tc.in))
			if err == nil {
				t.Fatal("want an error, got none")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q does not mention %q", err, tc.want)
			}
		})
	}
}

// Every manifest must carry the version it was rendered for and, per
// platform, the checksum of that platform's binary and no other.
func TestRenderPinsVersionAndHashes(t *testing.T) {
	files, err := Render("v9.9.9", fixtureSums(t))
	if err != nil {
		t.Fatal(err)
	}

	formula := files[FormulaPath]
	if !strings.Contains(formula, `version "9.9.9"`) {
		t.Errorf("formula does not pin 9.9.9:\n%s", formula)
	}
	for asset, hash := range map[string]string{
		assetDarwinAMD64: fixDarwinAMD64,
		assetDarwinARM64: fixDarwinARM64,
		assetLinuxAMD64:  fixLinuxAMD64,
		assetLinuxARM64:  fixLinuxARM64,
	} {
		want := "url \"" + repoURL + "/releases/download/v9.9.9/" + asset + "\"\n" +
			"      sha256 \"" + hash + "\""
		if !strings.Contains(formula, want) {
			t.Errorf("formula does not pair %s with its checksum; want:\n%s\ngot:\n%s", asset, want, formula)
		}
	}

	var scoop struct {
		Version      string `json:"version"`
		Bin          string `json:"bin"`
		Architecture map[string]struct {
			URL  string `json:"url"`
			Hash string `json:"hash"`
		} `json:"architecture"`
	}
	if err := json.Unmarshal([]byte(files[ScoopPath]), &scoop); err != nil {
		t.Fatalf("scoop manifest is not valid JSON: %v", err)
	}
	if scoop.Version != "9.9.9" {
		t.Errorf("scoop version: got %q, want 9.9.9", scoop.Version)
	}
	if got := scoop.Architecture["64bit"].Hash; got != fixWindowsAMD64 {
		t.Errorf("scoop 64bit hash: got %q, want the windows binary's %q", got, fixWindowsAMD64)
	}
	if !strings.Contains(scoop.Architecture["64bit"].URL, "/v9.9.9/"+assetWindowsAMD64) {
		t.Errorf("scoop url does not point at the release binary: %q", scoop.Architecture["64bit"].URL)
	}
	if scoop.Bin != "overwater.exe" {
		t.Errorf("scoop bin: got %q", scoop.Bin)
	}

	var installer struct {
		PackageVersion string `yaml:"PackageVersion"`
		InstallerType  string `yaml:"InstallerType"`
		Installers     []struct {
			Architecture    string `yaml:"Architecture"`
			InstallerURL    string `yaml:"InstallerUrl"`
			InstallerSha256 string `yaml:"InstallerSha256"`
		} `yaml:"Installers"`
	}
	if err := yaml.Unmarshal([]byte(files[WingetInstallPath]), &installer); err != nil {
		t.Fatalf("winget installer manifest is not valid YAML: %v", err)
	}
	if installer.PackageVersion != "9.9.9" {
		t.Errorf("winget installer version: got %q, want 9.9.9", installer.PackageVersion)
	}
	if len(installer.Installers) != 1 {
		t.Fatalf("winget installer count: got %d, want 1", len(installer.Installers))
	}
	if got, want := installer.Installers[0].InstallerSha256, strings.ToUpper(fixWindowsAMD64); got != want {
		t.Errorf("winget sha256: got %q, want %q", got, want)
	}
	if !strings.Contains(installer.Installers[0].InstallerURL, "/v9.9.9/"+assetWindowsAMD64) {
		t.Errorf("winget url does not point at the release binary: %q", installer.Installers[0].InstallerURL)
	}

	for _, path := range []string{WingetVersionPath, WingetInstallPath, WingetLocalePath} {
		var doc struct {
			PackageIdentifier string `yaml:"PackageIdentifier"`
			PackageVersion    string `yaml:"PackageVersion"`
			ManifestVersion   string `yaml:"ManifestVersion"`
		}
		if err := yaml.Unmarshal([]byte(files[path]), &doc); err != nil {
			t.Fatalf("%s is not valid YAML: %v", path, err)
		}
		if doc.PackageIdentifier != "MithrilBytes.Overwater" {
			t.Errorf("%s identifier: got %q", path, doc.PackageIdentifier)
		}
		if doc.PackageVersion != "9.9.9" {
			t.Errorf("%s version: got %q, want 9.9.9", path, doc.PackageVersion)
		}
		if doc.ManifestVersion != wingetManifestVer {
			t.Errorf("%s manifest version: got %q, want %s", path, doc.ManifestVersion, wingetManifestVer)
		}
	}
}

// A platform with no SHA256SUMS line must stop the run before anything
// is written.
func TestRenderRejectsMissingAsset(t *testing.T) {
	for _, asset := range Assets {
		t.Run(asset, func(t *testing.T) {
			sums := fixtureSums(t)
			delete(sums, asset)
			files, err := Render("v9.9.9", sums)
			if err == nil {
				t.Fatalf("want an error for the missing %s, got %d files", asset, len(files))
			}
			if !strings.Contains(err.Error(), asset) {
				t.Errorf("error %q does not name %s", err, asset)
			}
			if files != nil {
				t.Error("a failed render returned files")
			}
		})
	}
}

func TestRenderRejectsBadVersion(t *testing.T) {
	for _, v := range []string{"", "2.1", "latest", "v2.1.0-rc1", "2.1.0.0"} {
		if _, err := Render(v, fixtureSums(t)); err == nil {
			t.Errorf("version %q was accepted", v)
		}
	}
	// A leading v is optional; the URLs get one either way.
	files, err := Render("9.9.9", fixtureSums(t))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(files[FormulaPath], "/releases/download/v9.9.9/") {
		t.Error("a bare version did not produce a v prefixed release URL")
	}
}

func TestBumpFlake(t *testing.T) {
	const src = "{\n  outputs = x:\n    let\n      version = \"1.0.0\";\n" +
		"      vendorHash = \"sha256-keepme\";\n    in x;\n}\n"
	got, err := BumpFlake(src, "v9.9.9")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, `version = "9.9.9";`) {
		t.Errorf("version not bumped:\n%s", got)
	}
	if !strings.Contains(got, `vendorHash = "sha256-keepme";`) {
		t.Errorf("vendorHash was rewritten:\n%s", got)
	}
	again, err := BumpFlake(got, "v9.9.9")
	if err != nil {
		t.Fatal(err)
	}
	if again != got {
		t.Error("BumpFlake is not idempotent")
	}
	if _, err := BumpFlake("{ }\n", "v9.9.9"); err == nil {
		t.Error("a flake with no version line was accepted")
	}
	if _, err := BumpFlake(src, "latest"); err == nil {
		t.Error("a version that is not vX.Y.Z was accepted")
	}
}

// newRepo makes a directory Sync can write into: only flake.nix has to
// exist beforehand, because Sync edits it rather than generating it.
func newRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	src, err := os.ReadFile(filepath.Join("..", "..", FlakePath))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, FlakePath), src, 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestSyncWritesAndIsIdempotent(t *testing.T) {
	dir := newRepo(t)
	sums := fixtureSums(t)

	changed, err := Sync(dir, "v9.9.9", sums)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{FormulaPath, FlakePath, ScoopPath, WingetInstallPath, WingetLocalePath, WingetVersionPath}
	for _, path := range want {
		body, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(path)))
		if err != nil {
			t.Fatalf("%s was not written: %v", path, err)
		}
		if !strings.Contains(string(body), "9.9.9") {
			t.Errorf("%s does not mention the release:\n%s", path, body)
		}
	}
	if len(changed) != len(want) {
		t.Errorf("changed %d files, want %d: %v", len(changed), len(want), changed)
	}

	before := snapshot(t, dir, want)
	changed, err = Sync(dir, "v9.9.9", sums)
	if err != nil {
		t.Fatal(err)
	}
	if len(changed) != 0 {
		t.Errorf("a second sync of the same release rewrote %v", changed)
	}
	for path, body := range snapshot(t, dir, want) {
		if !bytes.Equal(body, before[path]) {
			t.Errorf("%s changed on the second sync", path)
		}
	}
}

func TestSyncNeedsFlake(t *testing.T) {
	if _, err := Sync(t.TempDir(), "v9.9.9", fixtureSums(t)); err == nil {
		t.Fatal("syncing a directory with no flake.nix succeeded")
	}
}

func snapshot(t *testing.T, dir string, paths []string) map[string][]byte {
	t.Helper()
	out := make(map[string][]byte, len(paths))
	for _, p := range paths {
		body, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(p)))
		if err != nil {
			t.Fatal(err)
		}
		out[p] = body
	}
	return out
}

func TestRun(t *testing.T) {
	dir := newRepo(t)
	sums := filepath.Join("testdata", "SHA256SUMS")

	var out, errOut bytes.Buffer
	if code := Run([]string{"-version", "v9.9.9", "-sums", sums, "-dir", dir}, nil, &out, &errOut); code != 0 {
		t.Fatalf("exit %d, stderr: %s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "updated 6 files for v9.9.9") {
		t.Errorf("stdout: %q", out.String())
	}

	out.Reset()
	if code := Run([]string{"-version", "v9.9.9", "-sums", sums, "-dir", dir}, nil, &out, &errOut); code != 0 {
		t.Fatalf("second run exited %d", code)
	}
	if !strings.Contains(out.String(), "already describe") {
		t.Errorf("second run stdout: %q", out.String())
	}
}

func TestRunReadsStdin(t *testing.T) {
	dir := newRepo(t)
	body, err := os.ReadFile(filepath.Join("testdata", "SHA256SUMS"))
	if err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	code := Run([]string{"-version", "v9.9.9", "-sums", "-", "-dir", dir}, bytes.NewReader(body), &out, &errOut)
	if code != 0 {
		t.Fatalf("exit %d, stderr: %s", code, errOut.String())
	}
}

func TestRunRejectsBadInvocations(t *testing.T) {
	dir := newRepo(t)
	sums := filepath.Join("testdata", "SHA256SUMS")
	cases := map[string][]string{
		"no arguments":      {},
		"no sums":           {"-version", "v9.9.9", "-dir", dir},
		"no version":        {"-sums", sums, "-dir", dir},
		"bad version":       {"-version", "latest", "-sums", sums, "-dir", dir},
		"missing sums file": {"-version", "v9.9.9", "-sums", filepath.Join("testdata", "absent"), "-dir", dir},
		"unknown flag":      {"-relase", "v9.9.9"},
		"stray argument":    {"-version", "v9.9.9", "-sums", sums, "-dir", dir, "extra"},
	}
	for name, args := range cases {
		t.Run(name, func(t *testing.T) {
			var out, errOut bytes.Buffer
			if code := Run(args, nil, &out, &errOut); code != 2 {
				t.Fatalf("exit %d, want 2; stdout %q", code, out.String())
			}
			if errOut.Len() == 0 {
				t.Error("nothing was written to stderr")
			}
		})
	}
}
