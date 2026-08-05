package cli

import (
	"bytes"
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
		{"catalog stub", []string{"catalog"}, ExitError, "", "catalog is not implemented yet"},
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

func TestUsageListsEveryCommand(t *testing.T) {
	var out bytes.Buffer
	printUsage(&out)
	for _, cmd := range commands {
		if !strings.Contains(out.String(), cmd.name) {
			t.Errorf("usage output is missing command %q", cmd.name)
		}
	}
}
