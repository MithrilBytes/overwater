package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// writeCatalogDir builds a two entry catalog source: test-model, whose
// prices litellm disagrees with, and loner-model, which litellm does
// not know at all.
func writeCatalogDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "models"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "VERSION"), []byte("2026-01-01\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	entries := map[string]string{
		"test-model":  "id: test-model\nprovider: testco\ninput_per_mtok: 1\noutput_per_mtok: 2\ncontext_window: 1000\ntier: mid\nreleased: \"2025-01-01\"\nsource: https://example.com/pricing\n",
		"loner-model": "id: loner-model\nprovider: testco\ninput_per_mtok: 5\noutput_per_mtok: 10\ncontext_window: 1000\ntier: mid\nreleased: \"2025-01-01\"\nsource: https://example.com/pricing\n",
	}
	for id, body := range entries {
		if err := os.WriteFile(filepath.Join(dir, "models", id+".yaml"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// The nightly price watch drives catalog diff. The whole happy path:
// report drift, then -write applies it, bumps VERSION, rebuilds
// catalog.json, and snapshots history.
func TestCatalogDiffDrift(t *testing.T) {
	dir := writeCatalogDir(t)
	litellm := filepath.Join(t.TempDir(), "litellm.json")
	prices := `{
  "test-model": {"input_cost_per_token": 2e-06, "output_cost_per_token": 4e-06, "max_input_tokens": 2000},
  "unrelated-model": {"input_cost_per_token": 1e-06}
}`
	if err := os.WriteFile(litellm, []byte(prices), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := Run([]string{"catalog", "diff", "-dir", dir, litellm}, &stdout, &stderr)
	if code != ExitClean {
		t.Fatalf("exit = %d, stderr = %q", code, stderr.String())
	}
	for _, want := range []string{
		"test-model: ours 1/2, litellm 2/4",
		"note: test-model: context window ours 1000, litellm 2000",
		"1 drifted, 1 notes, 1 not in litellm, 2 checked",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("stdout is missing %q:\n%s", want, stdout.String())
		}
	}

	stdout.Reset()
	code = Run([]string{"catalog", "diff", "-dir", dir, "-write", litellm}, &stdout, &stderr)
	if code != ExitClean {
		t.Fatalf("-write exit = %d, stderr = %q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "updated 1 entries") {
		t.Errorf("stdout = %q, want the update count", stdout.String())
	}
	entry, err := os.ReadFile(filepath.Join(dir, "models", "test-model.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"input_per_mtok: 2\n", "output_per_mtok: 4\n"} {
		if !strings.Contains(string(entry), want) {
			t.Errorf("entry = %q, want %q applied", entry, want)
		}
	}
	rawVersion, err := os.ReadFile(filepath.Join(dir, "VERSION"))
	if err != nil {
		t.Fatal(err)
	}
	version := strings.TrimSpace(string(rawVersion))
	if !regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`).MatchString(version) || version == "2026-01-01" {
		t.Errorf("VERSION = %q, want it bumped to a fresh date", version)
	}
	built, err := os.ReadFile(filepath.Join(dir, "catalog.json"))
	if err != nil {
		t.Fatalf("catalog.json was not rebuilt: %v", err)
	}
	if !strings.Contains(string(built), `"test-model"`) || !strings.Contains(string(built), version) {
		t.Errorf("catalog.json = %q, want the entry at the bumped version", built)
	}
	if _, err := os.Stat(filepath.Join(dir, "history", version+".json")); err != nil {
		t.Errorf("history snapshot missing: %v", err)
	}

	// The applied prices agree with litellm now: a second diff is calm.
	stdout.Reset()
	if code := Run([]string{"catalog", "diff", "-dir", dir, litellm}, &stdout, &stderr); code != ExitClean {
		t.Fatalf("post-write exit = %d, stderr = %q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "0 drifted") {
		t.Errorf("stdout = %q, want no drift after the write", stdout.String())
	}
}

// A drift free catalog with -write must not bump anything.
func TestCatalogDiffNoDrift(t *testing.T) {
	dir := writeCatalogDir(t)
	litellm := filepath.Join(t.TempDir(), "litellm.json")
	prices := `{"test-model": {"input_cost_per_token": 1e-06, "output_cost_per_token": 2e-06}}`
	if err := os.WriteFile(litellm, []byte(prices), 0o644); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"catalog", "diff", "-dir", dir, "-write", litellm}, &stdout, &stderr); code != ExitClean {
		t.Fatalf("exit = %d, stderr = %q", code, stderr.String())
	}
	raw, err := os.ReadFile(filepath.Join(dir, "VERSION"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(raw)) != "2026-01-01" {
		t.Errorf("VERSION = %q, want it untouched without drift", raw)
	}
	if _, err := os.Stat(filepath.Join(dir, "catalog.json")); !os.IsNotExist(err) {
		t.Errorf("catalog.json written without drift (stat err %v)", err)
	}
}
