package cli

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func runEvalTo(t *testing.T, fixture string) (string, string, string) {
	t.Helper()
	dir := t.TempDir()
	var stdout, stderr bytes.Buffer
	code := Run([]string{"eval", "-o", dir, fixturePath(fixture)}, &stdout, &stderr)
	if code != ExitClean {
		t.Fatalf("eval exit = %d, stderr = %q", code, stderr.String())
	}
	return dir, stdout.String(), stderr.String()
}

// pyCompile syntax checks a generated script when python3 is around.
func pyCompile(t *testing.T, path string) {
	t.Helper()
	py, err := exec.LookPath("python3")
	if err != nil {
		t.Log("python3 not on PATH; skipping compile check")
		return
	}
	out, err := exec.Command(py, "-m", "py_compile", path).CombinedOutput()
	if err != nil {
		t.Errorf("generated script does not compile: %v\n%s", err, out)
	}
}

func TestEvalGeneratesAnthropicScript(t *testing.T) {
	dir, stdout, _ := runEvalTo(t, "py-extraction")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("got %d scripts, want 1: %v", len(entries), entries)
	}
	path := filepath.Join(dir, entries[0].Name())
	if !strings.Contains(stdout, path) {
		t.Errorf("stdout = %q, want the written path", stdout)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`CURRENT = "claude-opus-5"`,
		`CANDIDATE = "claude-haiku-4-5"`,
		"import anthropic",
		"If eval agreement drops below 97%, stay put",
	} {
		if !strings.Contains(string(body), want) {
			t.Errorf("script is missing %q", want)
		}
	}
	pyCompile(t, path)
}

func TestEvalGeneratesEmbeddingScript(t *testing.T) {
	dir, _, _ := runEvalTo(t, "rag-frontier-embeddings")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("got %d scripts, want 1", len(entries))
	}
	path := filepath.Join(dir, entries[0].Name())
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`CURRENT = "text-embedding-3-large"`,
		`CANDIDATE = "text-embedding-3-small"`,
		"nearest neighbor agreement",
	} {
		if !strings.Contains(string(body), want) {
			t.Errorf("script is missing %q", want)
		}
	}
	pyCompile(t, path)
}

// The batch finding nominates the same model on a cheaper endpoint, so
// there is nothing to A/B; the deprecated model finding gets a script.
func TestEvalSkipsSameModelCandidates(t *testing.T) {
	dir, _, stderr := runEvalTo(t, "node-cron-summarizer")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("got %d scripts, want only the deprecated model eval", len(entries))
	}
	if !strings.Contains(stderr, "skipped batch-on-realtime") {
		t.Errorf("stderr = %q, want the batch finding skipped with a reason", stderr)
	}
	body, err := os.ReadFile(filepath.Join(dir, entries[0].Name()))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), `CANDIDATE = "gpt-5-mini"`) {
		t.Errorf("script should target the successor model")
	}
}

func TestEvalCleanAppHasNothingToDo(t *testing.T) {
	_, stdout, _ := runEvalTo(t, "clean-app")
	if !strings.Contains(stdout, "Nothing to eval.") {
		t.Errorf("stdout = %q", stdout)
	}
}
