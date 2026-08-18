package cli

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// The composite action ships from this repo, so its hardening is pinned
// here: inputs reach bash only through env, tool output is escaped into
// <pre> where a repo controlled path can close nothing, and the shipped
// input defaults have to produce a run that exits on its own merits.

type actionStep struct {
	Name string `yaml:"name"`
	Run  string `yaml:"run"`
}

func actionSteps(t *testing.T) []actionStep {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "action.yml"))
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Runs struct {
			Steps []actionStep `yaml:"steps"`
		} `yaml:"runs"`
	}
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("action.yml does not parse: %v", err)
	}
	if len(doc.Runs.Steps) == 0 {
		t.Fatal("action.yml has no steps")
	}
	return doc.Runs.Steps
}

// An input interpolated into a run script is re parsed as shell.
func TestActionInputsNotInlined(t *testing.T) {
	re := regexp.MustCompile(`\$\{\{\s*inputs\.`)
	for _, s := range actionSteps(t) {
		if re.MatchString(s.Run) {
			t.Errorf("step %q interpolates an input into its script:\n%s", s.Name, s.Run)
		}
	}
}

// No fence is safe: a scanned path may hold a newline and a run of
// backticks one longer than whichever length the report picked.
func TestActionReportNotFenced(t *testing.T) {
	for _, s := range actionSteps(t) {
		if s.Name != "scan" {
			continue
		}
		if strings.Contains(s.Run, "echo '```") {
			t.Error("the report still fences tool output in backticks")
		}
		return
	}
	t.Fatal("action.yml has no scan step")
}

// actionInputDefaults is what the Action ships with, so a test can run a
// step under exactly the combination an unconfigured caller gets.
func actionInputDefaults(t *testing.T) map[string]string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "action.yml"))
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Inputs map[string]struct {
			Default string `yaml:"default"`
		} `yaml:"inputs"`
	}
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("action.yml does not parse: %v", err)
	}
	if len(doc.Inputs) == 0 {
		t.Fatal("action.yml declares no inputs")
	}
	out := map[string]string{}
	for name, in := range doc.Inputs {
		out[name] = in.Default
	}
	return out
}

// runScanStep runs the scan step's own script with a stand in binary in
// place of overwater, and returns the RUNNER_TEMP it wrote into. Inputs
// come from action.yml's declared defaults; overrides win.
func runScanStep(t *testing.T, stub string, overrides map[string]string) string {
	t.Helper()
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash is not on PATH")
	}
	var run string
	for _, s := range actionSteps(t) {
		if s.Name == "scan" {
			run = s.Run
		}
	}
	if run == "" {
		t.Fatal("action.yml has no scan step")
	}
	dir := t.TempDir()
	script := filepath.Join(dir, "step.sh")
	if err := os.WriteFile(script, []byte(run), 0o644); err != nil {
		t.Fatal(err)
	}
	stubPath := filepath.Join(dir, "overwater-stub")
	if err := os.WriteFile(stubPath, []byte("#!/bin/sh\n"+stub+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	env := append(os.Environ(),
		"OVERWATER_BIN="+stubPath,
		"RUNNER_TEMP="+dir,
		"GITHUB_STEP_SUMMARY="+filepath.Join(dir, "summary.md"),
		"GITHUB_OUTPUT="+filepath.Join(dir, "output.txt"),
	)
	for name, value := range actionInputDefaults(t) {
		if v, ok := overrides[name]; ok {
			value = v
		}
		env = append(env, "INPUT_"+strings.ToUpper(strings.ReplaceAll(name, "-", "_"))+"="+value)
	}
	cmd := exec.Command(bash, script)
	cmd.Env = env
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("scan step failed: %v\n%s", err, out)
	}
	return dir
}

// echoArgs is a stand in overwater that reports the argv the step built.
const echoArgs = `printf '%s\n' "$@"`

func stepArgs(t *testing.T, dir string) []string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(dir, "overwater-out.txt"))
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimRight(string(raw), "\n"), "\n")
}

// The Action's own defaults, through the Action's own script, into the
// real CLI: an unconfigured caller scanning a clean repository has to
// exit 0. Naming --fail-on new with no baseline for it to be new
// against is exit 2, an operational failure, on a correct scan.
func TestActionDefaultsScanClean(t *testing.T) {
	dir := runScanStep(t, echoArgs, map[string]string{"path": fixturePath("clean-app")})
	args := stepArgs(t, dir)
	var stdout, stderr bytes.Buffer
	if code := Run(args, &stdout, &stderr); code != ExitClean {
		t.Fatalf("overwater %s exit = %d, want %d; stderr = %q",
			strings.Join(args, " "), code, ExitClean, stderr.String())
	}
}

// The same defaults over a repository that has findings: advisory, and
// still exit 0. The ratchet has nothing to ratchet against yet.
func TestActionDefaultsAdvisory(t *testing.T) {
	dir := runScanStep(t, echoArgs, map[string]string{"path": fixturePath("ts-chat-firehose")})
	args := stepArgs(t, dir)
	var stdout, stderr bytes.Buffer
	if code := Run(args, &stdout, &stderr); code != ExitClean {
		t.Fatalf("overwater %s exit = %d, want %d; stderr = %q",
			strings.Join(args, " "), code, ExitClean, stderr.String())
	}
}

// Dropping the default policy must not drop a configured one: with a
// baseline the ratchet is passed through, and an explicit --fail-on any
// still fails without one.
func TestActionKeepsConfiguredPolicy(t *testing.T) {
	dir := runScanStep(t, echoArgs, map[string]string{
		"path":     fixturePath("clean-app"),
		"baseline": ".overwater.json",
	})
	joined := strings.Join(stepArgs(t, dir), " ")
	if !strings.Contains(joined, "--baseline .overwater.json") || !strings.Contains(joined, "--fail-on new") {
		t.Errorf("args = %q, want the baseline and the policy", joined)
	}

	dir = runScanStep(t, echoArgs, map[string]string{
		"path":    fixturePath("ts-chat-firehose"),
		"fail-on": "any",
	})
	args := stepArgs(t, dir)
	var stdout, stderr bytes.Buffer
	if code := Run(args, &stdout, &stderr); code != ExitFindings {
		t.Fatalf("overwater %s exit = %d, want %d; stderr = %q",
			strings.Join(args, " "), code, ExitFindings, stderr.String())
	}
}

// A scanned path is repo controlled, and POSIX allows a newline in one.
// Everything the tool printed has to land inside one <pre> block with
// & < > escaped, where Markdown is not parsed and the scanned
// repository cannot write the body of the bot's own comment.
func TestActionReportEscapesToolOutput(t *testing.T) {
	payload := `a\n` + "````" + `\nINJECTED </pre> <img src=x> & co\nb.py:3\n`
	dir := runScanStep(t, "printf '"+payload+"'", map[string]string{"path": "."})
	raw, err := os.ReadFile(filepath.Join(dir, "overwater-report.md"))
	if err != nil {
		t.Fatal(err)
	}
	report := string(raw)
	if got, want := strings.Count(report, "<pre>"), 1; got != want {
		t.Fatalf("report has %d opening <pre>, want %d:\n%s", got, want, report)
	}
	if got, want := strings.Count(report, "</pre>"), 1; got != want {
		t.Fatalf("report has %d closing tags, want %d; the output closed the block:\n%s", got, want, report)
	}
	for _, want := range []string{"&lt;/pre&gt;", "&lt;img src=x&gt;", "&amp; co"} {
		if !strings.Contains(report, want) {
			t.Errorf("report does not escape %q:\n%s", want, report)
		}
	}
}

// bash -n is the closest thing to running the action locally.
func TestActionScriptsParse(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash is not on PATH")
	}
	for _, s := range actionSteps(t) {
		if s.Run == "" {
			continue
		}
		path := filepath.Join(t.TempDir(), "step.sh")
		if err := os.WriteFile(path, []byte(s.Run), 0o644); err != nil {
			t.Fatal(err)
		}
		if out, err := exec.Command(bash, "-n", path).CombinedOutput(); err != nil {
			t.Errorf("step %q does not parse as bash: %v\n%s", s.Name, err, out)
		}
	}
}
