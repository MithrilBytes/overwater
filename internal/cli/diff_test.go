package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeDoc(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

const oldDoc = `{
  "catalog_version": "2026-08-05",
  "calls_per_month": 10000,
  "findings": [
    {"rule": "frontier-extraction", "file": "a.py", "model": "gpt-5.1", "monthly_usd": 100, "candidate_model": "gpt-5-mini"},
    {"rule": "cron-batch", "file": "jobs/s.js", "model": "gpt-5", "monthly_usd": 50, "candidate_model": "gpt-5-mini"},
    {"rule": "stable-rule", "file": "same.py", "model": "gpt-5", "monthly_usd": 10}
  ]
}`

const newDoc = `{
  "catalog_version": "2026-08-06",
  "calls_per_month": 10000,
  "findings": [
    {"rule": "frontier-extraction", "file": "a.py", "model": "gpt-5.1", "monthly_usd": 160, "candidate_model": "gpt-5-mini"},
    {"rule": "stable-rule", "file": "same.py", "model": "gpt-5", "monthly_usd": 10},
    {"rule": "deprecated-model", "file": "old.js", "model": "text-davinci-003", "monthly_usd": 30}
  ]
}`

func TestDiffReportsChanges(t *testing.T) {
	dir := t.TempDir()
	oldPath := writeDoc(t, dir, "old.json", oldDoc)
	newPath := writeDoc(t, dir, "new.json", newDoc)
	var stdout, stderr bytes.Buffer
	code := Run([]string{"diff", oldPath, newPath}, &stdout, &stderr)
	if code != ExitClean {
		t.Fatalf("exit = %d, stderr = %q", code, stderr.String())
	}
	want := "cost: frontier-extraction at a.py (candidate gpt-5-mini) ~$100/mo -> ~$160/mo\n" +
		"disappeared: cron-batch at jobs/s.js (candidate gpt-5-mini) ~$50/mo\n" +
		"appeared: deprecated-model at old.js ~$30/mo\n" +
		"total: ~$160/mo -> ~$200/mo (+$40/mo)\n"
	if stdout.String() != want {
		t.Errorf("diff output mismatch\n got: %q\nwant: %q", stdout.String(), want)
	}
}

func TestDiffIdenticalReportsOnlyTotal(t *testing.T) {
	dir := t.TempDir()
	oldPath := writeDoc(t, dir, "old.json", oldDoc)
	newPath := writeDoc(t, dir, "new.json", oldDoc)
	var stdout, stderr bytes.Buffer
	code := Run([]string{"diff", oldPath, newPath}, &stdout, &stderr)
	if code != ExitClean {
		t.Fatalf("exit = %d, stderr = %q", code, stderr.String())
	}
	if stdout.String() != "total: ~$160/mo -> ~$160/mo (+$0/mo)\n" {
		t.Errorf("stdout = %q, want only the flat total line", stdout.String())
	}
}

func TestDiffCostDropShowsNegativeDelta(t *testing.T) {
	dir := t.TempDir()
	oldPath := writeDoc(t, dir, "old.json", oldDoc)
	empty := writeDoc(t, dir, "empty.json", `{"catalog_version": "2026-08-06", "findings": []}`)
	var stdout, stderr bytes.Buffer
	code := Run([]string{"diff", oldPath, empty}, &stdout, &stderr)
	if code != ExitClean {
		t.Fatalf("exit = %d, stderr = %q", code, stderr.String())
	}
	out := stdout.String()
	if strings.Count(out, "disappeared:") != 3 {
		t.Errorf("stdout = %q, want three disappeared lines", out)
	}
	if !strings.Contains(out, "total: ~$160/mo -> ~$0/mo (-$160/mo)") {
		t.Errorf("stdout = %q, want a negative delta total", out)
	}
}

// Valid JSON that is not a scan report ({}) must not diff as an empty
// report and pass: it exits 2 and names the file.
func TestDiffRejectsNonReportJSON(t *testing.T) {
	dir := t.TempDir()
	good := writeDoc(t, dir, "good.json", oldDoc)
	for name, doc := range map[string]string{
		"empty object": `{}`,
		"other schema": `{"widgets": [1, 2, 3]}`,
		"null":         `null`,
	} {
		t.Run(name, func(t *testing.T) {
			bad := writeDoc(t, dir, "bad.json", doc)
			var stdout, stderr bytes.Buffer
			if code := Run([]string{"diff", bad, good}, &stdout, &stderr); code != ExitError {
				t.Fatalf("exit = %d, want %d; stdout = %q", code, ExitError, stdout.String())
			}
			if !strings.Contains(stderr.String(), bad) || !strings.Contains(stderr.String(), "not scan --json output") {
				t.Errorf("stderr = %q, want the file named as not scan --json output", stderr.String())
			}
		})
	}
}

func TestDiffOperationalErrors(t *testing.T) {
	dir := t.TempDir()
	good := writeDoc(t, dir, "good.json", oldDoc)
	bad := writeDoc(t, dir, "bad.json", "not json")
	cases := []struct {
		name string
		args []string
	}{
		{"missing file", []string{"diff", filepath.Join(dir, "absent.json"), good}},
		{"invalid json", []string{"diff", good, bad}},
		{"one argument", []string{"diff", good}},
		{"three arguments", []string{"diff", good, good, good}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if code := Run(tc.args, &stdout, &stderr); code != ExitError {
				t.Fatalf("exit = %d, want %d; stderr = %q", code, ExitError, stderr.String())
			}
			if stderr.Len() == 0 {
				t.Error("stderr is empty, want an error message")
			}
		})
	}
}
