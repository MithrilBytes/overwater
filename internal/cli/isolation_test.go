package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// configuredRoots writes two repos that each hold one deprecated model
// call, giving each the .overwater.yaml named for it; an empty string
// means no config file at all.
func configuredRoots(t *testing.T, cfgA, cfgB string) (string, string) {
	t.Helper()
	dir := t.TempDir()
	a := filepath.Join(dir, "svc-a")
	b := filepath.Join(dir, "svc-b")
	for i, d := range []string{a, b} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
		writeRepoFile(t, d, "call.js", legacyCall)
		if cfg := []string{cfgA, cfgB}[i]; cfg != "" {
			writeRepoFile(t, d, configName, cfg)
		}
	}
	return a, b
}

// scanJSON runs scan -json with arbitrary arguments.
func scanJSON(t *testing.T, args ...string) (int, jsonReport, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := Run(append([]string{"scan", "-json"}, args...), &stdout, &stderr)
	var report jsonReport
	if stdout.Len() > 0 {
		if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
			t.Fatalf("bad JSON: %v\n%s", err, stdout.String())
		}
	}
	return code, report, stderr.String()
}

// One engine per process, filtered in place per root, made a repo's
// disable list outlive the repo: whichever root carried it silenced the
// rule for every root scanned after it. Same repos, same command,
// opposite verdict from argument order alone.
func TestConfigDisableStaysInItsRoot(t *testing.T) {
	a, b := configuredRoots(t, "disable: [deprecated-model]\n", "")
	for _, order := range [][]string{{a, b}, {b, a}} {
		code, report, stderr := scanJSON(t, append([]string{"-fail-on", "any"}, order...)...)
		if code != ExitFindings {
			t.Fatalf("scan %v exit = %d, want %d; stderr = %q", order, code, ExitFindings, stderr)
		}
		if len(report.Findings) != 1 || !strings.HasPrefix(report.Findings[0].File, "svc-b/") {
			t.Errorf("scan %v findings = %+v, want only svc-b's; svc-a's disable is svc-a's alone",
				order, report.Findings)
		}
	}
}

// The same leak in fleet, where one pipeline served the whole list
// file: the first repo's disable decided what the rest of the fleet
// could still be caught for.
func TestFleetConfigStaysPerRepo(t *testing.T) {
	a, b := configuredRoots(t, "disable: [deprecated-model]\n", "")
	dir := t.TempDir()
	lists := map[string]string{
		"ab": a + "\n" + b + "\n",
		"ba": b + "\n" + a + "\n",
	}
	results := map[string]string{}
	for name, content := range lists {
		path := filepath.Join(dir, name+".txt")
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		code, stdout, stderr := runFleetArgs(t, "-fail-on", "any", path)
		if code != ExitFindings {
			t.Fatalf("fleet %s exit = %d, want %d; stderr = %q", name, code, ExitFindings, stderr)
		}
		for _, line := range strings.Split(stdout, "\n") {
			if strings.HasPrefix(line, "fleet: ") {
				results[name] = line
			}
		}
	}
	if results["ab"] != results["ba"] {
		t.Errorf("rollups differ by list order: %q vs %q", results["ab"], results["ba"])
	}
	if !strings.Contains(results["ab"], "2 repos, 1 findings") {
		t.Errorf("rollup = %q, want the unconfigured repo's finding counted", results["ab"])
	}
}

// A per repo volume leaked into the shared estimates too, so a merged
// report could print a header at one root's volume over a body priced
// at another's. One report gets one volume, whatever the order.
func TestVolumeDisagreementUsesDefault(t *testing.T) {
	a, b := configuredRoots(t, "volume: 1000000\n", "")
	_, solo, _ := scanJSON(t, b)
	if len(solo.Findings) != 1 || solo.CallsPerMonth != 10000 {
		t.Fatalf("control run: calls %d, findings %+v", solo.CallsPerMonth, solo.Findings)
	}
	for _, order := range [][]string{{a, b}, {b, a}} {
		code, report, stderr := scanJSON(t, order...)
		if code != ExitClean {
			t.Fatalf("scan %v exit = %d, stderr = %q", order, code, ExitClean)
		}
		if report.CallsPerMonth != solo.CallsPerMonth {
			t.Errorf("scan %v header = %d calls, want the default %d once the roots disagree",
				order, report.CallsPerMonth, solo.CallsPerMonth)
		}
		if len(report.Findings) != 2 {
			t.Fatalf("scan %v findings = %+v, want one per root", order, report.Findings)
		}
		for _, f := range report.Findings {
			if f.MonthlyUSD != solo.Findings[0].MonthlyUSD {
				t.Errorf("scan %v priced %s at $%d under a $%d header basis; the header must match the body",
					order, f.File, f.MonthlyUSD, solo.Findings[0].MonthlyUSD)
			}
		}
		if !strings.Contains(stderr, "disagrees") {
			t.Errorf("stderr = %q, want the ignored per root volume named", stderr)
		}
	}
}

// Roots that all want the same volume still get it: the header names it
// and every number under it uses it.
func TestVolumeHonoredWhenRootsAgree(t *testing.T) {
	cfg := "volume: 1000000\n"
	a, b := configuredRoots(t, cfg, cfg)
	_, solo, _ := scanJSON(t, a)
	if solo.CallsPerMonth != 1000000 || len(solo.Findings) != 1 {
		t.Fatalf("single root: calls %d, findings %+v", solo.CallsPerMonth, solo.Findings)
	}
	code, report, stderr := scanJSON(t, a, b)
	if code != ExitClean {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
	if report.CallsPerMonth != solo.CallsPerMonth {
		t.Errorf("header = %d calls, want %d from the agreeing configs", report.CallsPerMonth, solo.CallsPerMonth)
	}
	if len(report.Findings) != 2 {
		t.Fatalf("findings = %+v, want one per root", report.Findings)
	}
	for _, f := range report.Findings {
		if f.MonthlyUSD != solo.Findings[0].MonthlyUSD {
			t.Errorf("priced %s at $%d, want $%d at the agreed volume", f.File, f.MonthlyUSD, solo.Findings[0].MonthlyUSD)
		}
	}
	if strings.Contains(stderr, "disagrees") {
		t.Errorf("stderr = %q, want no disagreement note when the roots agree", stderr)
	}
}

// A threshold is a per repo knob like any other and must not survive
// into the next root.
func TestConfigThresholdStaysInItsRoot(t *testing.T) {
	a, b := configuredRoots(t, "thresholds:\n  deprecated-model:\n    min_duplicate_sites: 99\n", "")
	code, report, stderr := scanJSON(t, "-fail-on", "any", a, b)
	if code != ExitFindings {
		t.Fatalf("exit = %d, want %d; stderr = %q", code, ExitFindings, stderr)
	}
	if len(report.Findings) != 1 || !strings.HasPrefix(report.Findings[0].File, "svc-b/") {
		t.Errorf("findings = %+v, want only svc-b's; svc-a's threshold is svc-a's alone", report.Findings)
	}
}
