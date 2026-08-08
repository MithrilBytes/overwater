package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestRunRendersStdinSubjects(t *testing.T) {
	stdin := strings.NewReader("feat(cli): add explain\nchore: pin the action\n")
	var stdout, stderr bytes.Buffer
	code := run([]string{"-prev", "v2.1.0", "-tag", "v2.1.1", "-repo", "MithrilBytes/overwater"}, stdin, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit %d, stderr: %s", code, stderr.String())
	}
	for _, want := range []string{
		"### Features\n\n- cli: add explain\n",
		"### Chores\n\n- pin the action\n",
		"compare/v2.1.0...v2.1.1",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("output missing %q:\n%s", want, stdout.String())
		}
	}
}

// A tag that moves no commits still gets notes, not an error.
func TestRunAcceptsAnEmptyLog(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"-prev", "v2.1.0", "-tag", "v2.1.1", "-repo", "r/r"}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit %d, stderr: %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "No commits since v2.1.0.") {
		t.Errorf("output does not say the log was empty:\n%s", stdout.String())
	}
}

func TestRunRejectsMissingFlags(t *testing.T) {
	for _, args := range [][]string{
		{"-tag", "v1.0.0"},
		{"-repo", "r/r"},
		{"-nope"},
	} {
		var stdout, stderr bytes.Buffer
		if code := run(args, strings.NewReader(""), &stdout, &stderr); code != 2 {
			t.Errorf("run(%v) = %d, want 2", args, code)
		}
		if stdout.Len() != 0 {
			t.Errorf("run(%v) wrote notes anyway: %s", args, stdout.String())
		}
	}
}

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, errors.New("pipe broke") }

func TestRunReportsAnUnreadableStdin(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"-tag", "v1", "-repo", "r/r"}, failingReader{}, &stdout, &stderr); code != 2 {
		t.Errorf("exit %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "pipe broke") {
		t.Errorf("stderr does not name the failure: %s", stderr.String())
	}
}
