package cli

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestScanSummaryLine(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run([]string{"scan", "-summary", fixturePath("node-cron-summarizer")}, &stdout, &stderr)
	if code != ExitClean {
		t.Fatalf("exit = %d, stderr = %q", code, stderr.String())
	}
	out := strings.TrimSpace(stdout.String())
	if strings.Count(out, "\n") != 0 {
		t.Fatalf("summary should be one line, got %q", out)
	}
	if !strings.Contains(out, "2 findings") {
		t.Errorf("summary = %q, want the findings count", out)
	}
}

func TestScanWritesHTMLAndCSV(t *testing.T) {
	dir := t.TempDir()
	htmlPath := filepath.Join(dir, "report.html")
	csvPath := filepath.Join(dir, "findings.csv")
	var stdout, stderr bytes.Buffer
	code := Run([]string{"scan", "-html", htmlPath, "-csv", csvPath, fixturePath("py-extraction")}, &stdout, &stderr)
	if code != ExitClean {
		t.Fatalf("exit = %d, stderr = %q", code, stderr.String())
	}
	html, err := os.ReadFile(htmlPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(html), "claude-opus-5") {
		t.Errorf("html report is missing the finding")
	}
	csv, err := os.ReadFile(csvPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(csv), "frontier-extraction") {
		t.Errorf("csv is missing the finding: %s", csv)
	}
}

func TestVersionPrintsDev(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"version"}, &stdout, &stderr); code != ExitClean {
		t.Fatalf("exit = %d, stderr = %q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "overwater dev") {
		t.Errorf("stdout = %q, want the dev version", stdout.String())
	}
}

func TestEvalDraftsPromptSets(t *testing.T) {
	dir := t.TempDir()
	var stdout, stderr bytes.Buffer
	code := Run([]string{"eval", "-o", dir, "-draft-prompts", fixturePath("py-extraction")}, &stdout, &stderr)
	if code != ExitClean {
		t.Fatalf("exit = %d, stderr = %q", code, stderr.String())
	}
	matches, err := filepath.Glob(filepath.Join(dir, "*.prompts.jsonl"))
	if err != nil || len(matches) != 1 {
		t.Fatalf("drafted prompt sets = %v (err %v), want exactly 1", matches, err)
	}
	f, err := os.Open(matches[0])
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	lines := 0
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		var row struct {
			Prompt string `json:"prompt"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &row); err != nil {
			t.Fatalf("line %d is not valid JSONL: %v", lines+1, err)
		}
		if len(row.Prompt) < 20 {
			t.Errorf("drafted prompt too short: %q", row.Prompt)
		}
		lines++
	}
	if lines == 0 {
		t.Fatal("drafted prompt set is empty")
	}
	if !strings.Contains(stdout.String(), "drafted") {
		t.Errorf("stdout = %q, want a drafted note", stdout.String())
	}
}
