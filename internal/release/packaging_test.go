package release

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// Nothing in CI compiles the release workflow, so what it has to keep
// doing is pinned here.

func repoFile(t *testing.T, parts ...string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(append([]string{"..", ".."}, parts...)...))
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

type workflow struct {
	Permissions map[string]string `yaml:"permissions"`
	Jobs        map[string]struct {
		Permissions map[string]string `yaml:"permissions"`
		Steps       []struct {
			Name string            `yaml:"name"`
			Uses string            `yaml:"uses"`
			Run  string            `yaml:"run"`
			If   string            `yaml:"if"`
			With map[string]string `yaml:"with"`
		} `yaml:"steps"`
	} `yaml:"jobs"`
}

func readWorkflow(t *testing.T, name string) workflow {
	t.Helper()
	var w workflow
	if err := yaml.Unmarshal([]byte(repoFile(t, ".github", "workflows", name)), &w); err != nil {
		t.Fatalf("%s does not parse: %v", name, err)
	}
	if len(w.Jobs) == 0 {
		t.Fatalf("%s has no jobs", name)
	}
	return w
}

// The notes are generated now, so a hardcoded --notes would silently
// bypass this package.
func TestReleaseWorkflowUsesGeneratedNotes(t *testing.T) {
	w := readWorkflow(t, "release.yml")
	var run string
	for _, s := range w.Jobs["release"].Steps {
		run += s.Run + "\n"
	}
	if !strings.Contains(run, "--notes-file") || regexp.MustCompile(`--notes\s`).MatchString(run) {
		t.Error("release.yml does not create the release from a --notes-file")
	}
	if !strings.Contains(run, "go run ./internal/release/cmd") {
		t.Error("release.yml does not render the notes with ./internal/release/cmd")
	}
	if !strings.Contains(run, "git log") {
		t.Error("release.yml does not feed the notes from git log")
	}
}
