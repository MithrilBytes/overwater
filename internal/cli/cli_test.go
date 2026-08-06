package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRun(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		wantCode   int
		wantStdout string
		wantStderr string
	}{
		{"no arguments", nil, ExitError, "", "Usage:"},
		{"help", []string{"help"}, ExitClean, "Usage:", ""},
		{"short help flag", []string{"-h"}, ExitClean, "Usage:", ""},
		{"long help flag", []string{"--help"}, ExitClean, "Usage:", ""},
		{"unknown command", []string{"water"}, ExitError, "", `unknown command "water"`},
		{"scan with a missing root", []string{"scan", "a", "b"}, ExitError, "", "scan a"},
		{"eval with two paths", []string{"eval", "a", "b"}, ExitError, "", "at most one path"},
		{"catalog without subcommand", []string{"catalog"}, ExitError, "", "Subcommands:"},
		{"catalog unknown subcommand", []string{"catalog", "nuke"}, ExitError, "", `unknown subcommand "nuke"`},
		{"catalog show", []string{"catalog", "show"}, ExitClean, "claude-haiku-4-5", ""},
		{"catalog build with a bad dir", []string{"catalog", "build", "-dir", "does-not-exist"}, ExitError, "", "read catalog version"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := Run(tt.args, &stdout, &stderr)
			if code != tt.wantCode {
				t.Fatalf("Run(%v) exit code = %d, want %d", tt.args, code, tt.wantCode)
			}
			checkStream(t, "stdout", stdout.String(), tt.wantStdout)
			checkStream(t, "stderr", stderr.String(), tt.wantStderr)
		})
	}
}

// checkStream asserts got contains want, where an empty want means the
// stream must be empty: help never leaks to stderr, errors never to stdout.
func checkStream(t *testing.T, label, got, want string) {
	t.Helper()
	if want == "" {
		if got != "" {
			t.Errorf("%s = %q, want empty", label, got)
		}
		return
	}
	if !strings.Contains(got, want) {
		t.Errorf("%s = %q, want it to contain %q", label, got, want)
	}
}

func fixturePath(name string) string {
	return filepath.Join("..", "..", "fixtures", name)
}

func TestScanCleanAppPrintsNullVerdict(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run([]string{"scan", fixturePath("clean-app")}, &stdout, &stderr)
	if code != ExitClean {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Keep the models you have.") {
		t.Errorf("stdout = %q, want the null verdict", stdout.String())
	}
}

func TestScanFirehosePrintsFindings(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run([]string{"scan", fixturePath("ts-chat-firehose")}, &stdout, &stderr)
	if code != ExitClean {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr.String())
	}
	for _, want := range []string{
		"Call site: app/api/chat/route.ts:7",
		"claude-haiku-4-5, same capability tier for this task class, ~$27/mo",
		"No prompt caching on a 1,191-token repeated system prompt",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("stdout is missing %q:\n%s", want, stdout.String())
		}
	}
}

func TestScanJSONWithVolumeOverride(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run([]string{"scan", "-json", "-volume", "20000", fixturePath("py-extraction")}, &stdout, &stderr)
	if code != ExitClean {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr.String())
	}
	var report struct {
		CallsPerMonth int `json:"calls_per_month"`
		Findings      []struct {
			Rule       string `json:"rule"`
			MonthlyUSD int    `json:"monthly_usd"`
		} `json:"findings"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("bad JSON: %v\n%s", err, stdout.String())
	}
	if report.CallsPerMonth != 20000 {
		t.Errorf("calls_per_month = %d, want 20000", report.CallsPerMonth)
	}
	if len(report.Findings) != 1 || report.Findings[0].Rule != "frontier-extraction" {
		t.Fatalf("findings = %+v", report.Findings)
	}
	if report.Findings[0].MonthlyUSD != 252 {
		t.Errorf("monthly_usd = %d, want 252 at doubled volume", report.Findings[0].MonthlyUSD)
	}
}

func TestScanWritesSARIF(t *testing.T) {
	out := filepath.Join(t.TempDir(), "findings.sarif")
	var stdout, stderr bytes.Buffer
	code := Run([]string{"scan", "-sarif", out, fixturePath("ts-chat-firehose")}, &stdout, &stderr)
	if code != ExitClean {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr.String())
	}
	raw, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Runs []struct {
			Results []struct {
				RuleID    string `json:"ruleId"`
				Locations []struct {
					PhysicalLocation struct {
						ArtifactLocation struct {
							URI string `json:"uri"`
						} `json:"artifactLocation"`
					} `json:"physicalLocation"`
				} `json:"locations"`
			} `json:"results"`
		} `json:"runs"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("SARIF is not valid JSON: %v", err)
	}
	if len(doc.Runs) != 1 || len(doc.Runs[0].Results) == 0 {
		t.Fatalf("SARIF runs = %+v, want one run with results", doc.Runs)
	}
	found := false
	for _, r := range doc.Runs[0].Results {
		if r.RuleID == "unbounded-max-tokens" &&
			len(r.Locations) == 1 &&
			r.Locations[0].PhysicalLocation.ArtifactLocation.URI == "app/api/chat/route.ts" {
			found = true
		}
	}
	if !found {
		t.Errorf("SARIF is missing the unbounded-max-tokens result at app/api/chat/route.ts:\n%s", raw)
	}
}

func TestScanWritesModelsMD(t *testing.T) {
	dir := t.TempDir()
	var stdout, stderr bytes.Buffer
	code := Run([]string{"scan", "-models-md", dir}, &stdout, &stderr)
	if code != ExitClean {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr.String())
	}
	data, err := os.ReadFile(filepath.Join(dir, "MODELS.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "# Overwater verdict") ||
		!strings.Contains(string(data), "Keep the models you have.") {
		t.Errorf("MODELS.md = %q", data)
	}
}

func TestScanMissingRepoFails(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run([]string{"scan", filepath.Join("does", "not", "exist")}, &stdout, &stderr)
	if code != ExitError {
		t.Fatalf("exit code = %d, want %d", code, ExitError)
	}
	if !strings.Contains(stderr.String(), "scan") {
		t.Errorf("stderr = %q, want a scan error", stderr.String())
	}
}

func TestCatalogBuildWritesOutput(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "models"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "VERSION"), []byte("2026-01-01\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	entry := "id: test-model\nprovider: testco\ninput_per_mtok: 1\noutput_per_mtok: 2\ncontext_window: 1000\ntier: mid\nreleased: \"2025-01-01\"\nsource: https://example.com/pricing\n"
	if err := os.WriteFile(filepath.Join(dir, "models", "test-model.yaml"), []byte(entry), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "catalog.json")
	var stdout, stderr bytes.Buffer
	code := Run([]string{"catalog", "build", "-dir", dir, "-o", out}, &stdout, &stderr)
	if code != ExitClean {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr.String())
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"test-model"`) {
		t.Errorf("catalog.json is missing the entry: %s", data)
	}
}

func TestUsageListsEveryCommand(t *testing.T) {
	var out bytes.Buffer
	printUsage(&out)
	for _, cmd := range commands {
		if !strings.Contains(out.String(), cmd.name) {
			t.Errorf("usage output is missing command %q", cmd.name)
		}
	}
}
