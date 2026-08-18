package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// repoWith writes a temp repo whose only finding without config is
// deprecated-model, plus an optional .overwater.yaml.
func repoWith(t *testing.T, config string) string {
	t.Helper()
	dir := t.TempDir()
	src := "MODEL = \"text-davinci-003\"\n"
	if err := os.WriteFile(filepath.Join(dir, "legacy.py"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	if config != "" {
		if err := os.WriteFile(filepath.Join(dir, configName), []byte(config), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// jsonFinding is the shared decode target for scan -json across these
// tests. Named rather than inline so tests can pass one around.
type jsonFinding struct {
	Rule       string `json:"rule"`
	File       string `json:"file"`
	Line       int    `json:"line"`
	Model      string `json:"model"`
	Archetype  string `json:"archetype"`
	Confidence string `json:"confidence"`
	MonthlyUSD int    `json:"monthly_usd"`
	Volume     int    `json:"volume"`
}

type jsonReport struct {
	CallsPerMonth int           `json:"calls_per_month"`
	Findings      []jsonFinding `json:"findings"`
}

// scanRepo runs scan -json and parses stdout when the run succeeded.
func scanRepo(t *testing.T, dir string, extra ...string) (int, jsonReport, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	args := append([]string{"scan", "-json"}, extra...)
	args = append(args, dir)
	code := Run(args, &stdout, &stderr)
	var report jsonReport
	if stdout.Len() > 0 {
		if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
			t.Fatalf("bad JSON: %v\n%s", err, stdout.String())
		}
	}
	return code, report, stderr.String()
}

func TestConfigDisable(t *testing.T) {
	code, report, stderr := scanRepo(t, repoWith(t, ""))
	if code != ExitClean || len(report.Findings) != 1 || report.Findings[0].Rule != "deprecated-model" {
		t.Fatalf("control run: code %d, findings %+v, stderr %q", code, report.Findings, stderr)
	}
	code, report, stderr = scanRepo(t, repoWith(t, "disable: [deprecated-model]\n"))
	if code != ExitClean {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr)
	}
	if len(report.Findings) != 0 {
		t.Errorf("findings = %+v, want none with the rule disabled", report.Findings)
	}
}

func TestConfigVolumeLosesToFlag(t *testing.T) {
	_, base, _ := scanRepo(t, repoWith(t, ""))
	if len(base.Findings) != 1 || base.Findings[0].MonthlyUSD <= 0 {
		t.Fatalf("control run findings = %+v", base.Findings)
	}
	code, doubled, stderr := scanRepo(t, repoWith(t, "volume: 20000\n"))
	if code != ExitClean {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr)
	}
	if doubled.CallsPerMonth != 20000 {
		t.Errorf("calls_per_month = %d, want 20000 from config", doubled.CallsPerMonth)
	}
	if want := 2 * base.Findings[0].MonthlyUSD; doubled.Findings[0].MonthlyUSD != want {
		t.Errorf("monthly_usd = %d, want %d at doubled volume", doubled.Findings[0].MonthlyUSD, want)
	}
	_, flagged, _ := scanRepo(t, repoWith(t, "volume: 20000\n"), "-volume", "10000")
	if flagged.CallsPerMonth != 10000 || flagged.Findings[0].MonthlyUSD != base.Findings[0].MonthlyUSD {
		t.Errorf("with --volume 10000: calls %d, monthly %d; the flag must beat the config",
			flagged.CallsPerMonth, flagged.Findings[0].MonthlyUSD)
	}
}

func TestConfigBudget(t *testing.T) {
	code, report, stderr := scanRepo(t, repoWith(t, "budget_monthly_usd: 1\n"))
	if code != ExitFindings {
		t.Fatalf("exit code = %d, want %d for a blown budget; stderr = %q", code, ExitFindings, stderr)
	}
	if !strings.Contains(stderr, "exceeds budget_monthly_usd 1") {
		t.Errorf("stderr = %q, want the line naming total and budget", stderr)
	}
	if len(report.Findings) != 1 {
		t.Errorf("findings = %+v; a budget failure must not eat the report", report.Findings)
	}
	code, _, stderr = scanRepo(t, repoWith(t, "budget_monthly_usd: 100000\n"))
	if code != ExitClean || strings.Contains(stderr, "budget") {
		t.Errorf("under budget: code %d, stderr %q, want a quiet clean exit", code, stderr)
	}
}

// --fail-on none never exits 1, budget included: the line still prints,
// the code stays 0.
func TestFailOnNoneIgnoresBudget(t *testing.T) {
	code, _, stderr := scanRepo(t, repoWith(t, "budget_monthly_usd: 1\n"), "-fail-on", "none")
	if code != ExitClean {
		t.Fatalf("exit code = %d, want %d under --fail-on none; stderr = %q", code, ExitClean, stderr)
	}
	if !strings.Contains(stderr, "exceeds budget_monthly_usd 1") {
		t.Errorf("stderr = %q, want the budget line to still print", stderr)
	}
}

func TestConfigUnknownField(t *testing.T) {
	code, _, stderr := scanRepo(t, repoWith(t, "budget: 1\n"))
	if code != ExitError {
		t.Fatalf("exit code = %d, want %d for an unknown config field", code, ExitError)
	}
	if !strings.Contains(stderr, configName) {
		t.Errorf("stderr = %q, want it to name %s", stderr, configName)
	}
}

func TestConfigExcludeMatches(t *testing.T) {
	cfg := &repoConfig{Exclude: []string{"*.json", "reports", "fixtures/*.py", "third_party/"}}
	cases := []struct {
		file string
		want bool
	}{
		{"registry.json", true},              // a bare glob matches at the root
		{"api/models/registry.json", true},   // and at any depth
		{"reports/scan/out.txt", true},       // a bare name matches a directory segment
		{"fixtures/legacy.py", true},         // a slashed pattern matches the whole path
		{"fixtures/sub/legacy.py", false},    // and only the whole path
		{"third_party/vendor/call.py", true}, // a named directory covers its tree
		{"src/classify.py", false},
	}
	for _, tc := range cases {
		if got := cfg.excluded(tc.file); got != tc.want {
			t.Errorf("excluded(%q) = %v, want %v", tc.file, got, tc.want)
		}
	}
	var absent *repoConfig
	if absent.excluded("registry.json") {
		t.Error("a repo with no config excludes nothing")
	}
}

// A pattern that silently matches nothing is worse than no pattern: the
// repo believes the file is excluded and the findings keep arriving.
func TestConfigExcludeRejectsBadPattern(t *testing.T) {
	code, _, stderr := scanRepo(t, repoWith(t, "exclude: [\"[\"]\n"))
	if code != ExitError || !strings.Contains(stderr, `exclude pattern "["`) {
		t.Errorf("code %d, stderr %q, want exit 2 naming the bad pattern", code, stderr)
	}
	code, _, stderr = scanRepo(t, repoWith(t, "exclude: [\"**/*.json\"]\n"))
	if code != ExitError || !strings.Contains(stderr, "** is not special") {
		t.Errorf("code %d, stderr %q, want exit 2 pointing at the bare form", code, stderr)
	}
	code, _, stderr = scanRepo(t, repoWith(t, "exclude: [\"\"]\n"))
	if code != ExitError || !strings.Contains(stderr, "empty pattern") {
		t.Errorf("code %d, stderr %q, want exit 2 for an empty pattern", code, stderr)
	}
}

func TestConfigExcludeLoads(t *testing.T) {
	code, _, stderr := scanRepo(t, repoWith(t, "exclude:\n  - registries/*.json\n  - reports\n"))
	if code == ExitError {
		t.Fatalf("exit = %d, stderr = %q; a valid exclude list must load", code, stderr)
	}
}

func TestConfigUnknownThreshold(t *testing.T) {
	code, _, stderr := scanRepo(t, repoWith(t, "thresholds:\n  deprecated-model:\n    min_carrots: 3\n"))
	if code != ExitError || !strings.Contains(stderr, "min_carrots") {
		t.Errorf("code %d, stderr %q, want exit 2 naming the bad field", code, stderr)
	}
	code, _, stderr = scanRepo(t, repoWith(t, "thresholds:\n  no-such-rule:\n    min_retries: 1\n"))
	if code != ExitError || !strings.Contains(stderr, "no-such-rule") {
		t.Errorf("code %d, stderr %q, want exit 2 naming the missing rule", code, stderr)
	}
}

func TestConfigThreshold(t *testing.T) {
	prompt := strings.Repeat("Route the ticket to the right team with care. ", 130)
	src := "const SYSTEM = `" + prompt + "`;\n" + `
export async function chatWithUsers(text: string) {
  return client.messages.create({
    model: "claude-sonnet-5",
    max_tokens: 1000,
    system: SYSTEM,
    messages: [{ role: "user", content: text }],
  });
}
`
	write := func(config string) string {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "chat.ts"), []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
		if config != "" {
			if err := os.WriteFile(filepath.Join(dir, configName), []byte(config), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		return dir
	}
	_, report, _ := scanRepo(t, write(""))
	if len(report.Findings) != 1 || report.Findings[0].Rule != "uncached-system-prompt" {
		t.Fatalf("control run findings = %+v, want uncached-system-prompt", report.Findings)
	}
	code, report, stderr := scanRepo(t, write("thresholds:\n  uncached-system-prompt:\n    min_system_tokens: 100000\n"))
	if code != ExitClean {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr)
	}
	if len(report.Findings) != 0 {
		t.Errorf("findings = %+v, want none once the threshold is raised", report.Findings)
	}
}
