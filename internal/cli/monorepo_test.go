package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func twoRoots(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	a := filepath.Join(dir, "svc-a")
	b := filepath.Join(dir, "svc-b")
	for _, d := range []string{a, b} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeRepoFile(t, a, "classify.js", classifyCall)
	writeRepoFile(t, b, "legacy.js", legacyCall)
	return a, b
}

// Multiple roots merge into one report with each finding's file
// prefixed by its root's base name, and stderr counts per root.
func TestMultiRootPrefixes(t *testing.T) {
	a, b := twoRoots(t)
	code, stdout, stderr := runScanArgs(t, a, b)
	if code != ExitClean {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
	if !strings.Contains(stdout, "svc-a/classify.js") {
		t.Errorf("stdout = %q, want svc-a/classify.js prefixed", stdout)
	}
	if !strings.Contains(stdout, "svc-b/legacy.js") {
		t.Errorf("stdout = %q, want svc-b/legacy.js prefixed", stdout)
	}
	if !strings.Contains(stderr, a+": ") || !strings.Contains(stderr, b+": 1 findings") {
		t.Errorf("stderr = %q, want one findings count line per root", stderr)
	}
}

// The ratchet works unchanged over the merged, prefixed finding set.
func TestMultiRootRatchet(t *testing.T) {
	a, b := twoRoots(t)
	bl := filepath.Join(t.TempDir(), ".overwater.json")
	if code, _, stderr := runScanArgs(t, "-baseline", bl, "-update-baseline", a, b); code != ExitClean {
		t.Fatalf("update exit = %d, stderr = %q", code, stderr)
	}
	code, _, stderr := runScanArgs(t, "-baseline", bl, a, b)
	if code != ExitClean {
		t.Fatalf("baselined scan exit = %d, stderr = %q", code, stderr)
	}
	if !strings.Contains(stderr, "all baselined") {
		t.Errorf("stderr = %q, want an all baselined note", stderr)
	}
	writeRepoFile(t, b, "fresh.js", legacyCall)
	code, _, stderr = runScanArgs(t, "-baseline", bl, a, b)
	if code != ExitFindings {
		t.Fatalf("exit = %d, want %d; stderr = %q", code, ExitFindings, stderr)
	}
	if !strings.Contains(stderr, "new: deprecated-model at svc-b/fresh.js") {
		t.Errorf("stderr = %q, want the new finding named with its root prefix", stderr)
	}
}

// --models-md has no home when several roots merge into one report.
func TestMultiRootRejectsModelsMD(t *testing.T) {
	a, b := twoRoots(t)
	code, _, stderr := runScanArgs(t, "-models-md", a, b)
	if code != ExitError {
		t.Fatalf("exit = %d, want %d", code, ExitError)
	}
	if !strings.Contains(stderr, "single root") {
		t.Errorf("stderr = %q, want a single root complaint", stderr)
	}
}
