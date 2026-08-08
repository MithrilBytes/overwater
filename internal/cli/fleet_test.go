package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fleetFixture builds one clean repo, one repo with a single finding,
// and a list file naming both plus a missing path, comments, and blanks.
func fleetFixture(t *testing.T) (clean, hot, list string) {
	t.Helper()
	dir := t.TempDir()
	clean = filepath.Join(dir, "clean")
	hot = filepath.Join(dir, "hot")
	for _, d := range []string{clean, hot} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeRepoFile(t, clean, "main.go", "package main\n")
	writeRepoFile(t, hot, "legacy.js", legacyCall)
	list = filepath.Join(dir, "repos.txt")
	content := "# fleet under test\n\n" + clean + "\n" + hot + "\n" + filepath.Join(dir, "missing") + "\n"
	if err := os.WriteFile(list, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return clean, hot, list
}

func runFleetArgs(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := Run(append([]string{"fleet"}, args...), &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

func TestFleetScansAndRollsUp(t *testing.T) {
	clean, hot, list := fleetFixture(t)
	code, stdout, stderr := runFleetArgs(t, list)
	if code != ExitClean {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
	if !strings.Contains(stdout, clean+": 0 findings, ~$0/mo") {
		t.Errorf("stdout = %q, want the clean repo's zero line", stdout)
	}
	if !strings.Contains(stdout, hot+": 1 findings, ~$") {
		t.Errorf("stdout = %q, want the hot repo's findings line", stdout)
	}
	if !strings.Contains(stdout, "fleet: 2 repos, 1 findings, ~$") || !strings.Contains(stdout, ", 1 failed") {
		t.Errorf("stdout = %q, want a rollup counting 2 repos, 1 finding, 1 failure", stdout)
	}
	if !strings.Contains(stderr, "missing") {
		t.Errorf("stderr = %q, want the unreadable repo named", stderr)
	}
}

func TestFleetFailOnAny(t *testing.T) {
	_, _, list := fleetFixture(t)
	code, _, stderr := runFleetArgs(t, "-fail-on", "any", list)
	if code != ExitFindings {
		t.Fatalf("exit = %d, want %d; stderr = %q", code, ExitFindings, stderr)
	}
	if !strings.Contains(stderr, "failing under --fail-on any") {
		t.Errorf("stderr = %q, want the policy named", stderr)
	}
}

func TestFleetFailOnAnyWhenClean(t *testing.T) {
	dir := t.TempDir()
	clean := filepath.Join(dir, "clean")
	if err := os.MkdirAll(clean, 0o755); err != nil {
		t.Fatal(err)
	}
	writeRepoFile(t, clean, "main.go", "package main\n")
	list := filepath.Join(dir, "repos.txt")
	if err := os.WriteFile(list, []byte(clean+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, _, stderr := runFleetArgs(t, "-fail-on", "any", list); code != ExitClean {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
}

// A fleet where every repo fails to scan learned nothing: exit 2, not a
// clean rollup. Partial failures keep the run green; an empty list
// stays clean.
func TestFleetAllReposFail(t *testing.T) {
	dir := t.TempDir()
	list := filepath.Join(dir, "repos.txt")
	content := filepath.Join(dir, "gone-a") + "\n" + filepath.Join(dir, "gone-b") + "\n"
	if err := os.WriteFile(list, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr := runFleetArgs(t, list)
	if code != ExitError {
		t.Fatalf("exit = %d, want %d when zero repos scanned; stderr = %q", code, ExitError, stderr)
	}
	if !strings.Contains(stdout, "fleet: 0 repos") {
		t.Errorf("stdout = %q, want the rollup still printed", stdout)
	}
	if !strings.Contains(stderr, "all 2 repos failed to scan") {
		t.Errorf("stderr = %q, want the total failure named", stderr)
	}

	// An empty list is a no-op, not a failure.
	empty := filepath.Join(dir, "empty.txt")
	if err := os.WriteFile(empty, []byte("# nothing\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, _, stderr := runFleetArgs(t, empty); code != ExitClean {
		t.Errorf("empty list exit = %d, want clean; stderr = %q", code, stderr)
	}
}

func TestFleetOperationalErrors(t *testing.T) {
	_, _, list := fleetFixture(t)
	cases := []struct {
		name string
		args []string
	}{
		{"missing list file", []string{filepath.Join(t.TempDir(), "absent.txt")}},
		{"no list file", nil},
		{"unknown fail-on", []string{"-fail-on", "sometimes", list}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code, _, stderr := runFleetArgs(t, tc.args...)
			if code != ExitError {
				t.Fatalf("exit = %d, want %d; stderr = %q", code, ExitError, stderr)
			}
		})
	}
}
