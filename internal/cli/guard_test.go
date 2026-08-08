package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const classifyCall = `const client = new (require("openai"))();

async function classifyThing(text) {
  return client.chat.completions.create({
    model: "gpt-5.1",
    temperature: 0,
    max_tokens: 60,
    response_format: { type: "json_schema" },
    messages: [{ role: "user", content: text }],
  });
}
`

const legacyCall = `const client = new (require("openai"))();

async function fetchLegacy(text) {
  return client.completions.create({
    model: "text-davinci-003",
    max_tokens: 100,
    prompt: text,
  });
}
`

func runScanArgs(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := Run(append([]string{"scan"}, args...), &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

func writeRepoFile(t *testing.T, repo, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(repo, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// The full ratchet lifecycle: record, pass while baselined, survive
// line drift, fail on a new finding, prune on update after a fix.
func TestRatchetLifecycle(t *testing.T) {
	dir := t.TempDir()
	repo := filepath.Join(dir, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	bl := filepath.Join(dir, ".overwater.json")
	writeRepoFile(t, repo, "classify.js", classifyCall)

	// First run records the existing finding; recording never fails.
	code, _, stderr := runScanArgs(t, "-baseline", bl, "-update-baseline", repo)
	if code != ExitClean {
		t.Fatalf("update-baseline exit = %d, stderr = %q", code, stderr)
	}

	// Baselined findings pass.
	if code, _, stderr = runScanArgs(t, "-baseline", bl, repo); code != ExitClean {
		t.Fatalf("baselined scan exit = %d, stderr = %q", code, stderr)
	}
	if !strings.Contains(stderr, "all baselined") {
		t.Errorf("stderr = %q, want an all baselined note", stderr)
	}

	// Line drift must not churn the fingerprint.
	writeRepoFile(t, repo, "classify.js", "// project header\n// added later\n\n"+classifyCall)
	if code, _, stderr = runScanArgs(t, "-baseline", bl, repo); code != ExitClean {
		t.Fatalf("scan after line drift exit = %d, stderr = %q; the fingerprint broke on drift", code, stderr)
	}

	// A new finding fails the build and is named on stderr.
	writeRepoFile(t, repo, "legacy.js", legacyCall)
	code, _, stderr = runScanArgs(t, "-baseline", bl, repo)
	if code != ExitFindings {
		t.Fatalf("scan with new finding exit = %d, want %d", code, ExitFindings)
	}
	if !strings.Contains(stderr, "new: deprecated-model at legacy.js") {
		t.Errorf("stderr = %q, want the new finding named", stderr)
	}

	// Fix the original call, re-record: the fixed finding is pruned and
	// only the legacy one remains.
	writeRepoFile(t, repo, "classify.js", strings.ReplaceAll(classifyCall, "gpt-5.1", "gpt-5-nano"))
	if code, _, stderr = runScanArgs(t, "-baseline", bl, "-update-baseline", repo); code != ExitClean {
		t.Fatalf("re-record exit = %d, stderr = %q", code, stderr)
	}
	raw, err := os.ReadFile(bl)
	if err != nil {
		t.Fatal(err)
	}
	var recorded struct {
		Findings []struct {
			Rule string `json:"rule"`
			File string `json:"file"`
		} `json:"findings"`
	}
	if err := json.Unmarshal(raw, &recorded); err != nil {
		t.Fatal(err)
	}
	if len(recorded.Findings) != 1 || recorded.Findings[0].Rule != "deprecated-model" {
		t.Fatalf("baseline after fix = %+v, want only the legacy finding", recorded.Findings)
	}
	if code, _, stderr = runScanArgs(t, "-baseline", bl, repo); code != ExitClean {
		t.Fatalf("final scan exit = %d, stderr = %q", code, stderr)
	}
}

// rewriteRecorded stamps every baseline entry's recorded date, so tests
// can age or corrupt them.
func rewriteRecorded(t *testing.T, path, recorded string) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Version  int    `json:"version"`
		Commit   string `json:"commit,omitempty"`
		Findings []struct {
			Fingerprint string `json:"fingerprint"`
			Rule        string `json:"rule"`
			File        string `json:"file"`
			Model       string `json:"model"`
			Recorded    string `json:"recorded"`
		} `json:"findings"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	for i := range doc.Findings {
		doc.Findings[i].Recorded = recorded
	}
	edited, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, edited, 0o644); err != nil {
		t.Fatal(err)
	}
}

// Aging: entries past --max-baseline-age-days nag on stderr and never
// change the exit code; fresh entries stay quiet.
func TestAgingNags(t *testing.T) {
	dir := t.TempDir()
	repo := filepath.Join(dir, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	bl := filepath.Join(dir, ".overwater.json")
	writeRepoFile(t, repo, "classify.js", classifyCall)
	if code, _, stderr := runScanArgs(t, "-baseline", bl, "-update-baseline", repo); code != ExitClean {
		t.Fatalf("update exit = %d, stderr = %q", code, stderr)
	}

	// Freshly recorded entries stay quiet.
	code, _, stderr := runScanArgs(t, "-baseline", bl, "-max-baseline-age-days", "30", repo)
	if code != ExitClean {
		t.Fatalf("scan exit = %d, stderr = %q", code, stderr)
	}
	if strings.Contains(stderr, "day limit") {
		t.Errorf("fresh entries nagged: %q", stderr)
	}

	// Backdate every recorded stamp: the same scan nags, still clean.
	rewriteRecorded(t, bl, "2026-01-01")
	code, _, stderr = runScanArgs(t, "-baseline", bl, "-max-baseline-age-days", "30", repo)
	if code != ExitClean {
		t.Fatalf("aged scan exit = %d, want clean; nags must not move the exit code (stderr %q)", code, stderr)
	}
	if !strings.Contains(stderr, "past the 30 day limit") || !strings.Contains(stderr, "classify.js") {
		t.Errorf("stderr = %q, want an aging nag naming classify.js", stderr)
	}
}

// A date that does not parse nags instead of silently never aging, and
// still never moves the exit code.
func TestUnreadableDateNags(t *testing.T) {
	dir := t.TempDir()
	repo := filepath.Join(dir, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	writeRepoFile(t, repo, "classify.js", classifyCall)
	bl := filepath.Join(dir, ".overwater.json")
	if code, _, stderr := runScanArgs(t, "-baseline", bl, "-update-baseline", repo); code != ExitClean {
		t.Fatalf("update exit = %d, stderr = %q", code, stderr)
	}
	rewriteRecorded(t, bl, "not-a-date")
	code, _, stderr := runScanArgs(t, "-baseline", bl, "-max-baseline-age-days", "30", repo)
	if code != ExitClean {
		t.Fatalf("exit = %d, want clean; a bad date is a nag, not a failure (stderr %q)", code, stderr)
	}
	if !strings.Contains(stderr, `unreadable recorded date "not-a-date"`) || !strings.Contains(stderr, "classify.js") {
		t.Errorf("stderr = %q, want an unreadable date nag naming classify.js", stderr)
	}
}

// The age limit belongs to the baseline, not the failure policy: any
// and none nag like new, and the nag never moves the exit code.
func TestAgingNagsAnyPolicy(t *testing.T) {
	dir := t.TempDir()
	repo := filepath.Join(dir, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	writeRepoFile(t, repo, "classify.js", classifyCall)
	bl := filepath.Join(dir, ".overwater.json")
	if code, _, stderr := runScanArgs(t, "-baseline", bl, "-update-baseline", repo); code != ExitClean {
		t.Fatalf("update exit = %d, stderr = %q", code, stderr)
	}
	rewriteRecorded(t, bl, "2026-01-01")
	cases := []struct {
		failOn   string
		wantCode int
	}{
		{"any", ExitFindings}, // findings exist, so any fails; the nag rides along
		{"none", ExitClean},
	}
	for _, tc := range cases {
		t.Run(tc.failOn, func(t *testing.T) {
			code, _, stderr := runScanArgs(t, "-baseline", bl, "-max-baseline-age-days", "30", "-fail-on", tc.failOn, repo)
			if code != tc.wantCode {
				t.Fatalf("exit = %d, want %d; stderr = %q", code, tc.wantCode, stderr)
			}
			if !strings.Contains(stderr, "past the 30 day limit") || !strings.Contains(stderr, "classify.js") {
				t.Errorf("stderr = %q, want an aging nag naming classify.js under --fail-on %s", stderr, tc.failOn)
			}
		})
	}
}

func TestFailOnAnyFails(t *testing.T) {
	code, _, stderr := runScanArgs(t, "-fail-on", "any", fixturePath("ts-chat-firehose"))
	if code != ExitFindings {
		t.Fatalf("exit = %d, want %d; stderr = %q", code, ExitFindings, stderr)
	}
}

func TestFailOnAnyClean(t *testing.T) {
	if code, _, stderr := runScanArgs(t, "-fail-on", "any", fixturePath("clean-app")); code != ExitClean {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
}

func TestFailOnNonePasses(t *testing.T) {
	if code, _, _ := runScanArgs(t, "-fail-on", "none", fixturePath("ts-chat-firehose")); code != ExitClean {
		t.Fatalf("exit = %d, want clean under fail-on none", code)
	}
}

func TestFailOnNewNeedsBaseline(t *testing.T) {
	code, _, stderr := runScanArgs(t, "-fail-on", "new", fixturePath("clean-app"))
	if code != ExitError {
		t.Fatalf("exit = %d, want %d", code, ExitError)
	}
	if !strings.Contains(stderr, "needs --baseline") {
		t.Errorf("stderr = %q, want a pointer at --baseline", stderr)
	}
}

func TestUnknownFailOn(t *testing.T) {
	if code, _, _ := runScanArgs(t, "-fail-on", "sometimes", fixturePath("clean-app")); code != ExitError {
		t.Fatalf("exit = %d, want %d", code, ExitError)
	}
}

func TestInvalidBaseline(t *testing.T) {
	dir := t.TempDir()
	bl := filepath.Join(dir, ".overwater.json")
	if err := os.WriteFile(bl, []byte("not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, _, stderr := runScanArgs(t, "-baseline", bl, fixturePath("clean-app"))
	if code != ExitError {
		t.Fatalf("exit = %d, want %d; garbage baselines are never findings", code, ExitError)
	}
	if !strings.Contains(stderr, "not valid JSON") {
		t.Errorf("stderr = %q", stderr)
	}
}

// Recording a baseline never fails, even when the repo's config budget
// is blown: the budget line still prints, the file still lands, exit 0.
func TestUpdateBaselineIgnoresBudget(t *testing.T) {
	dir := t.TempDir()
	repo := filepath.Join(dir, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	writeRepoFile(t, repo, "classify.js", classifyCall)
	writeRepoFile(t, repo, ".overwater.yaml", "budget_monthly_usd: 1\n")
	bl := filepath.Join(dir, ".overwater.json")
	code, _, stderr := runScanArgs(t, "-baseline", bl, "-update-baseline", repo)
	if code != ExitClean {
		t.Fatalf("update-baseline exit = %d, want %d; stderr = %q", code, ExitClean, stderr)
	}
	if !strings.Contains(stderr, "exceeds budget_monthly_usd") {
		t.Errorf("stderr = %q, want the budget line to still print", stderr)
	}
	if _, err := os.Stat(bl); err != nil {
		t.Errorf("baseline was not written: %v", err)
	}
}

func TestMissingBaseline(t *testing.T) {
	dir := t.TempDir()
	code, _, stderr := runScanArgs(t, "-baseline", filepath.Join(dir, "absent.json"), fixturePath("clean-app"))
	if code != ExitError {
		t.Fatalf("exit = %d, want %d", code, ExitError)
	}
	if !strings.Contains(stderr, "update-baseline") {
		t.Errorf("stderr = %q, want a hint to record one", stderr)
	}
}
