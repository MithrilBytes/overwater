package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// twoFileRepo is one directory holding a frontier classification and a
// deprecated model call, so a scan of one file can be told from a scan
// of the directory.
func twoFileRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	writeRepoFile(t, repo, "classify.js", classifyCall)
	writeRepoFile(t, repo, "legacy.js", legacyCall)
	return repo
}

// A file root reports that file and leaves its neighbours alone. The
// config loader used to open the root as a directory and never name the
// path the user typed.
func TestScanFileRootScansOnlyThatFile(t *testing.T) {
	repo := twoFileRepo(t)
	code, stdout, stderr := runScanArgs(t, filepath.Join(repo, "legacy.js"))
	if code != ExitClean {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
	if !strings.Contains(stdout, "Call site: legacy.js:") {
		t.Errorf("stdout = %q, want the named file's finding", stdout)
	}
	if strings.Contains(stdout, "classify.js") {
		t.Errorf("stdout = %q, want nothing from the file next to it", stdout)
	}
	if strings.Contains(stderr, configName) {
		t.Errorf("stderr = %q, want no config loader error", stderr)
	}
}

// The findings policy is the same for a file as for a directory.
func TestScanFileRootFailsOnAny(t *testing.T) {
	repo := twoFileRepo(t)
	code, _, stderr := runScanArgs(t, "-fail-on", "any", filepath.Join(repo, "legacy.js"))
	if code != ExitFindings {
		t.Fatalf("exit = %d, want %d; stderr = %q", code, ExitFindings, stderr)
	}
	code, _, stderr = runScanArgs(t, "-fail-on", "any", filepath.Join(repo, "requirements.txt"))
	if code != ExitError {
		t.Fatalf("missing file exit = %d, want %d; stderr = %q", code, ExitError, stderr)
	}
	if !strings.Contains(stderr, "requirements.txt") {
		t.Errorf("stderr = %q, want the path the user typed", stderr)
	}
}

// The containing directory's .overwater.yaml applies, which is the only
// repo config a single file has.
func TestScanFileRootUsesDirectoryConfig(t *testing.T) {
	repo := twoFileRepo(t)
	writeRepoFile(t, repo, configName, "disable:\n  - deprecated-model\n")
	code, stdout, stderr := runScanArgs(t, "-fail-on", "any", filepath.Join(repo, "legacy.js"))
	if code != ExitClean {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
	if !strings.Contains(stdout, "Keep the models you have.") {
		t.Errorf("stdout = %q, want the disabled rule to stay quiet", stdout)
	}
	writeRepoFile(t, repo, configName, "no_such_key: 1\n")
	if code, _, _ := runScanArgs(t, filepath.Join(repo, "legacy.js")); code != ExitError {
		t.Errorf("exit = %d on a malformed config, want %d", code, ExitError)
	}
}

// A pre commit hook passes a list of files. Files from one directory
// merge into one scan of it: no repeated walk, no multi root prefix.
func TestScanFileRootsInOneDirectoryMerge(t *testing.T) {
	repo := twoFileRepo(t)
	code, stdout, stderr := runScanArgs(t, filepath.Join(repo, "legacy.js"), filepath.Join(repo, "classify.js"))
	if code != ExitClean {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
	if got := strings.Count(stdout, "Call site:"); got != 2 {
		t.Fatalf("got %d call sites, want one per named file:\n%s", got, stdout)
	}
	if !strings.Contains(stdout, "Call site: legacy.js:") || !strings.Contains(stdout, "Call site: classify.js:") {
		t.Errorf("stdout = %q, want both files unprefixed", stdout)
	}
}

// Files from different directories keep the multi root prefixes.
func TestScanFileRootsAcrossDirectories(t *testing.T) {
	a, b := twoRoots(t)
	code, stdout, stderr := runScanArgs(t, filepath.Join(a, "classify.js"), filepath.Join(b, "legacy.js"))
	if code != ExitClean {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
	for _, want := range []string{"svc-a/classify.js", "svc-b/legacy.js"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout = %q, want %s", stdout, want)
		}
	}
}

// MODELS.md is the repository's verdict; a one file scan does not write
// one.
func TestScanFileRootRejectsModelsMD(t *testing.T) {
	repo := twoFileRepo(t)
	code, _, stderr := runScanArgs(t, "-models-md", filepath.Join(repo, "legacy.js"))
	if code != ExitError {
		t.Fatalf("exit = %d, want %d", code, ExitError)
	}
	if !strings.Contains(stderr, "directory root") {
		t.Errorf("stderr = %q, want a directory root complaint", stderr)
	}
	if _, err := os.Stat(filepath.Join(repo, "MODELS.md")); !os.IsNotExist(err) {
		t.Errorf("MODELS.md exists, want none written")
	}
}
