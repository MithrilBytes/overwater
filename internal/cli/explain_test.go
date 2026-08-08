package cli

import (
	"bytes"
	"strings"
	"testing"
)

func runExplainArgs(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := Run(append([]string{"explain"}, args...), &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

// One rule prints its predicate under the keys its YAML is written
// with, then the sentences the verdict prints.
func TestExplainRule(t *testing.T) {
	code, stdout, stderr := runExplainArgs(t, "retry-amplification")
	if code != ExitClean {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
	for _, want := range []string{
		"retry-amplification (flag, medium confidence)",
		"Looks for:",
		"tier: frontier",
		"min_retries: 3",
		"Candidate: same model with a lower retry cap",
		"Tripwire:  If the provider's error rate",
		"Flag:      max_retries",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout is missing %q:\n%s", want, stdout)
		}
	}
	if stderr != "" {
		t.Errorf("stderr = %q, want empty", stderr)
	}
}

// A finding rule has no flag line to print.
func TestExplainNoFlagLine(t *testing.T) {
	code, stdout, stderr := runExplainArgs(t, "frontier-extraction")
	if code != ExitClean {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
	if !strings.Contains(stdout, "archetype: extraction, classification") {
		t.Errorf("stdout is missing the archetype list:\n%s", stdout)
	}
	if strings.Contains(stdout, "Flag:") {
		t.Errorf("stdout = %q, want no flag line on a finding rule", stdout)
	}
}

func TestExplainListsRules(t *testing.T) {
	code, stdout, stderr := runExplainArgs(t)
	if code != ExitClean {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
	for _, want := range []string{"deprecated-model", "retry-amplification", "uncached-system-prompt"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout is missing %q:\n%s", want, stdout)
		}
	}
	if stderr != "" {
		t.Errorf("stderr = %q, want empty", stderr)
	}
}

// An unknown id is exit 2 plus the ids that do exist.
func TestExplainUnknownID(t *testing.T) {
	code, stdout, stderr := runExplainArgs(t, "retry-amplifcation")
	if code != ExitError {
		t.Fatalf("exit = %d, want %d", code, ExitError)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want empty", stdout)
	}
	if !strings.Contains(stderr, `no rule "retry-amplifcation"`) {
		t.Errorf("stderr = %q, want the unknown id named", stderr)
	}
	if !strings.Contains(stderr, "retry-amplification") {
		t.Errorf("stderr = %q, want the real ids listed", stderr)
	}
}

func TestExplainTooManyIDs(t *testing.T) {
	code, _, stderr := runExplainArgs(t, "deprecated-model", "frontier-extraction")
	if code != ExitError {
		t.Fatalf("exit = %d, want %d", code, ExitError)
	}
	if !strings.Contains(stderr, "one rule id") {
		t.Errorf("stderr = %q, want a one id complaint", stderr)
	}
}

// Every rule explains itself. An empty note or tripwire cannot load,
// but a predicate that renders to nothing would print a headless block.
func TestExplainCoversEveryRule(t *testing.T) {
	_, list, _ := runExplainArgs(t)
	for _, line := range strings.Split(list, "\n") {
		id := strings.TrimSpace(line)
		if id == "" || strings.HasSuffix(id, ":") {
			continue
		}
		code, stdout, stderr := runExplainArgs(t, id)
		if code != ExitClean {
			t.Fatalf("%s: exit = %d, stderr = %q", id, code, stderr)
		}
		if !strings.Contains(stdout, "Looks for:") {
			t.Errorf("%s: no predicate printed:\n%s", id, stdout)
		}
	}
}
