package scan

import (
	"strings"
	"testing"
)

func TestMaskPreservesOffsets(t *testing.T) {
	content := "const A = `line one\nline two`;\n// trailing comment\n"
	m := maskFile("a.ts", content)
	if len(m.all) != len(content) || len(m.prose) != len(content) {
		t.Fatal("masking changed the length")
	}
	if strings.Count(m.all, "\n") != strings.Count(content, "\n") {
		t.Fatal("masking changed line structure")
	}
}

func TestMaskBlanksCommentsAndProse(t *testing.T) {
	long := strings.Repeat("prose about temperature settings ", 4)
	content := "// comment with max_tokens: 99\nconst SHORT = \"input_schema\";\nconst LONG = \"" + long + "\";\n"
	m := maskFile("a.ts", content)
	if strings.Contains(m.prose, "max_tokens") {
		t.Error("comment text survived prose masking")
	}
	if !strings.Contains(m.prose, "input_schema") {
		t.Error("short syntax string should survive prose masking")
	}
	if strings.Contains(m.prose, "temperature") {
		t.Error("long prose string survived prose masking")
	}
	if strings.Contains(m.all, "input_schema") {
		t.Error("string interior survived full masking")
	}
}

func TestMaskPythonTriples(t *testing.T) {
	content := "PROMPT = \"\"\"docstring with temperature: 0.7 inside and quite a lot of extra words\"\"\"\nx = 1  # temperature: 0.2\n"
	m := maskFile("a.py", content)
	if strings.Contains(m.prose, "temperature") {
		t.Errorf("prose = %q, want prompt and comment text blanked", m.prose)
	}
}

// Layer 2 reads the code view, so a model named in a comment is not a
// call site. It used to be: the reference matcher saw raw bytes, and a
// line like `// switched from gpt-4o to gpt-4o-mini` produced two priced
// sites that carried findings into the baseline.
//
// The second half is why the prose view cannot be used here. Prose blanks
// string interiors over sixty characters, and a REST endpoint that names
// the model in its path is longer than that while being a genuine call.
func TestModelRefsSkipCommentsAndKeepLongStrings(t *testing.T) {
	dir := writeTree(t, map[string]string{
		"commented.ts": `import Anthropic from "@anthropic-ai/sdk";
// We should probably use "claude-haiku-4-5" here one day.
/* Or even claude-opus-5, who knows. */
export const client = new Anthropic();
`,
		"endpoint.cs": `class Transcriber {
    void Run() {
        var url = "https://generativelanguage.googleapis.com/v1beta/models/gemini-2.5-flash:generateContent";
    }
}
`,
	})

	report, err := Analyze(dir, mustCatalog(t))
	if err != nil {
		t.Fatal(err)
	}

	for _, s := range report.Sites {
		if s.File == "commented.ts" {
			t.Errorf("comment produced a site: %s at line %d", s.Ref, s.Line)
		}
	}

	var endpoint []Site
	for _, s := range report.Sites {
		if s.File == "endpoint.cs" {
			endpoint = append(endpoint, s)
		}
	}
	if len(endpoint) != 1 || endpoint[0].ModelID != "gemini-2.5-flash" {
		t.Errorf("endpoint sites = %+v, want one gemini-2.5-flash from the URL", endpoint)
	}
}

// The unknown-model pattern needs the same left-edge guard the catalog
// path has. Go's RE2 counts a hyphen as a non word character, so \b
// matches inside a hyphenated identifier: an API key prefixed sk-proj-
// command-secret, a plugin named my-claude-code-plugin and a flag value
// tunnel-mcp-command-equals all reported as models. Twelve repositories
// in a real-repo sweep were flagged for nothing else.
func TestUnknownModelNeedsALeftBoundary(t *testing.T) {
	notModels := []string{
		"sk-proj-command-secret123456",
		"cp-command-id",
		"tunnel-mcp-command-equals",
		"my-claude-code-plugin",
		"npm-command-runner",
		"x-gpt-4o-header",
	}
	for _, s := range notModels {
		dir := writeTree(t, map[string]string{"a.go": "package a\n\nvar s = \"" + s + "\"\n"})
		report, err := Analyze(dir, mustCatalog(t))
		if err != nil {
			t.Fatal(err)
		}
		if len(report.Sites) != 0 {
			t.Errorf("%q produced %d sites, want none: %+v", s, len(report.Sites), report.Sites)
		}
	}

	// The guard must not cost a real reference its detection.
	for _, s := range []string{"gpt-4o", "claude-opus-5", "gemini-2.5-flash"} {
		dir := writeTree(t, map[string]string{"a.go": "package a\n\nvar s = \"" + s + "\"\n"})
		report, err := Analyze(dir, mustCatalog(t))
		if err != nil {
			t.Fatal(err)
		}
		if len(report.Sites) != 1 {
			t.Errorf("%q produced %d sites, want 1", s, len(report.Sites))
		}
	}
}
