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

// Rust, Kotlin script, Vue, Svelte, Gradle and Terraform used to fall
// through familyFor's default, which reads # as a comment and // as
// code. Layer 2 reads the code view, so every commented out call site
// in those languages was priced as live spend at the model named in
// the comment.
func TestMaskReadsSlashCommentsOutsideTheJavaFamily(t *testing.T) {
	cases := []struct{ file, content string }{
		{"main.rs", "// let m = \"claude-opus-4-1\";\nlet m = \"gpt-5\";\n"},
		{"build.gradle.kts", "// was: claude-opus-4-1\nval m = \"gpt-5\"\n"},
		{"App.vue", "//   model: \"claude-opus-4-1\",\nconst m = \"gpt-5\";\n"},
		{"Card.svelte", "// model: claude-opus-4-1\nconst m = \"gpt-5\";\n"},
		{"build.gradle", "// model = 'claude-opus-4-1'\ndef m = \"gpt-5\"\n"},
		{"main.tf", "// model = \"claude-opus-4-1\"\nvariable \"model\" { default = \"gpt-5\" }\n"},
	}
	for _, tc := range cases {
		code := maskCode(tc.content, scanSpans(tc.content, familyFor(tc.file)))
		if strings.Contains(code, "claude-opus-4-1") {
			t.Errorf("%s: commented out model survived the code view: %q", tc.file, code)
		}
		if !strings.Contains(code, "gpt-5") {
			t.Errorf("%s: live model id did not survive the code view: %q", tc.file, code)
		}
	}
}

// Terraform takes both comment spellings, so gaining // must not cost
// it #.
func TestMaskKeepsHashCommentsInTerraform(t *testing.T) {
	content := "# model = \"claude-opus-4-1\"\nvariable \"model\" { default = \"gpt-5\" }\n"
	code := maskCode(content, scanSpans(content, familyFor("main.tf")))
	if strings.Contains(code, "claude-opus-4-1") {
		t.Errorf("hash comment survived the code view: %q", code)
	}
}

// A Rust service writes its request body as r#"{"model": ...}"#, where
// the hash is half the delimiter and not a comment. Under the hash
// default the body vanished at that first hash, so the idiomatic
// spelling of a Rust call site was invisible while the escaped one was
// not. The multi line form is still a prompt, and still blanked.
func TestMaskRustRawStrings(t *testing.T) {
	content := "let body = r#\"{\"model\": \"gpt-5\", \"max_tokens\": 64}\"#;\n" +
		"let prompt = br##\"You are a careful reviewer.\nPrefer claude-opus-4-1 for hard cases.\"##;\n"
	code := maskCode(content, scanSpans(content, familyFor("main.rs")))
	if !strings.Contains(code, "gpt-5") || !strings.Contains(code, "max_tokens") {
		t.Errorf("single line raw body did not survive the code view: %q", code)
	}
	if strings.Contains(code, "claude-opus-4-1") {
		t.Errorf("multi line raw prompt survived the code view: %q", code)
	}

	m := maskFile("main.rs", content)
	if strings.Contains(m.all, "gpt-5") {
		t.Errorf("raw string interior survived full masking: %q", m.all)
	}
	if len(m.all) != len(content) || len(m.prose) != len(content) {
		t.Fatal("masking changed the length")
	}
}

// The same repro the whole finding came from: a dead Rust call site,
// reported at high confidence as money someone is spending.
func TestCommentedRustCallSiteIsNotASite(t *testing.T) {
	dir := writeTree(t, map[string]string{"main.rs": `fn main() {
    // let request = CreateMessage::builder().model("claude-opus-4-1").max_tokens(1024).build();
    println!("done");
}
`})

	report, err := Analyze(dir, mustCatalog(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Sites) != 0 {
		t.Errorf("commented out call produced %d sites, want none: %+v", len(report.Sites), report.Sites)
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
