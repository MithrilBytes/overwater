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

func TestFleetScansEveryRepoAndRollsUp(t *testing.T) {
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

func TestFleetFailOnAnyPassesWhenAllClean(t *testing.T) {
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
