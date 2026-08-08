package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// The composite action ships from this repo, so its hardening is pinned
// here: inputs reach bash only through env, and report fences are four
// backticks so a repo controlled path containing ``` cannot break out.

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

func TestActionReportFences(t *testing.T) {
	for _, s := range actionSteps(t) {
		if s.Name != "scan" {
			continue
		}
		if strings.Contains(s.Run, "echo '```'") {
			t.Error("the report still uses a three backtick fence")
		}
		if got := strings.Count(s.Run, "echo '````'"); got != 4 {
			t.Errorf("four backtick fence count = %d, want 4 (stdout and stderr blocks, open and close)", got)
		}
		return
	}
	t.Fatal("action.yml has no scan step")
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
