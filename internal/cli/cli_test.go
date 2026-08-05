package cli

import (
	"bytes"
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
		{"scan stub", []string{"scan"}, ExitError, "", "scan is not implemented yet"},
		{"eval stub", []string{"eval"}, ExitError, "", "eval is not implemented yet"},
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
