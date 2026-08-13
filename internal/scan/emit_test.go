package scan

import (
	"testing"
)

// Every case here came from a real repository in a 128 repo sweep, not
// from imagination. The comment on each says which shape it stands for.

func TestDocumentationNeverEmitsSites(t *testing.T) {
	// bragfile000: 803 sites, 220 findings, $27,350 a month, from front
	// matter recording which coding agent wrote each document. The
	// binary is a CLI that writes SQLite rows and never opens a socket.
	dir := writeTree(t, map[string]string{
		"decisions/DEC-002.md": `---
agent:
  id: claude-opus-4-7
---

# Embedded migrations

We considered claude-opus-5 for this and rejected it.
`,
		"README.md":   "Supported models: gpt-4o, claude-opus-5, gemini-2.5-pro.\n",
		"notes.rst":   "Uses claude-sonnet-5 internally.\n",
		"CHANGES.txt": "Switched from gpt-4o to gpt-4o-mini.\n",
	})
	report, err := Analyze(dir, mustCatalog(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Sites) != 0 {
		t.Errorf("documentation produced %d sites: %+v", len(report.Sites), report.Sites)
	}
}

func TestTestFilesNeverEmitSites(t *testing.T) {
	// openai/tunnel-client: the only known site in the repository was
	// gpt-5 inside a _test.go fixture simulating a JSON response. The
	// product makes no inference call; it is a tunnel control plane.
	dir := writeTree(t, map[string]string{
		"client_test.go":       "package a\n\nvar want = \"gpt-5\"\n",
		"src/handler.test.ts":  "const fixture = { model: \"claude-opus-5\" };\n",
		"src/handler.spec.ts":  "const fixture = { model: \"claude-opus-5\" };\n",
		"tests/test_client.py": "MODEL = \"claude-opus-5\"\n",
		"spec/client_spec.rb":  "MODEL = 'claude-opus-5'\n",
		"__tests__/fixture.js": "export const m = \"gpt-4o\";\n",
		"testdata/golden.json": "{\"model\": \"gpt-4o\"}\n",
		"e2e/flow.js":          "const m = \"gpt-4o\";\n",
	})
	report, err := Analyze(dir, mustCatalog(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Sites) != 0 {
		t.Errorf("test files produced %d sites: %+v", len(report.Sites), report.Sites)
	}
}

// A test file still informs the layers it always did; it just does not
// report a site of its own. A test that calls a wrapper is a caller.
func TestTestFilesRemainContext(t *testing.T) {
	dir := writeTree(t, map[string]string{
		"lib.py": `from anthropic import Anthropic

client = Anthropic()

def ask(q):
    return client.messages.create(model="claude-opus-5", messages=[{"role":"user","content":q}])
`,
		"test_lib.py": "from lib import ask\n\ndef test_one():\n    ask('a')\n\ndef test_two():\n    ask('b')\n",
	})
	report, err := Analyze(dir, mustCatalog(t))
	if err != nil {
		t.Fatal(err)
	}
	var site *Site
	for i := range report.Sites {
		if report.Sites[i].File == "lib.py" {
			site = &report.Sites[i]
		}
	}
	if site == nil {
		t.Fatalf("no site in lib.py: %+v", report.Sites)
	}
	if site.FanIn < 2 {
		t.Errorf("fan in = %d, want at least 2; the callers are in a test file "+
			"and must still count", site.FanIn)
	}
}

// The distinction that matters in configuration: a model bound to a key
// is read by the program, a model in a list is one option among many.
func TestConfigBindingsNotRosters(t *testing.T) {
	for _, tc := range []struct {
		name, file, body string
		want             int
	}{
		{
			// librechat-config-yaml: 4,964 sites, 1,364 findings, no
			// LLM call anywhere. Every one of them looked like this.
			name: "yaml roster",
			file: "librechat.yaml",
			body: "endpoints:\n  custom:\n    - name: Anthropic\n      models:\n        default:\n          - claude-3-5-haiku-20241022\n          - claude-3-5-sonnet-20241022\n",
			want: 0,
		},
		{
			name: "yaml binding",
			file: "env_config.yaml",
			body: "service: batcher\nmodel: gpt-5.1\ntimeout_seconds: 30\n",
			want: 1,
		},
		{
			name: "yaml binding under a parent key",
			file: "llm.yaml",
			body: "llm:\n  model: gpt-5-mini\n  max_completion_tokens: 700\n",
			want: 1,
		},
		{
			name: "toml binding",
			file: "defaults.toml",
			body: "[runtime]\nworkers = 8\n\n[llm]\nmodel = \"claude-opus-4-1\"\n",
			want: 1,
		},
		{
			// The key is "name"; the section is what makes it a model.
			name: "ini binding by section",
			file: "settings.ini",
			body: "[service]\nname = doc-pipeline\n\n[model]\nname = mistral-large-2411\n",
			want: 1,
		},
		{
			// bragfile000 again: a provenance field, not a model choice.
			name: "non model key",
			file: "questions.yaml",
			body: "questions:\n  - id: q1\n    raised_by: claude-opus-4-8\n",
			want: 0,
		},
		{
			name: "inline list under a model key is still a roster",
			file: "opts.yaml",
			body: "models: [gpt-4o, gpt-4o-mini]\n",
			want: 0,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := writeTree(t, map[string]string{tc.file: tc.body})
			report, err := Analyze(dir, mustCatalog(t))
			if err != nil {
				t.Fatal(err)
			}
			known := 0
			for _, s := range report.Sites {
				if s.Known {
					known++
				}
			}
			if known != tc.want {
				t.Errorf("known sites = %d, want %d: %+v", known, tc.want, report.Sites)
			}
		})
	}
}

// metamask-storybook: five o3 sites, all of them a minifier's variable
// name inside a committed Storybook bundle.
func TestShortIDNeedsContext(t *testing.T) {
	dir := writeTree(t, map[string]string{
		"bundle.js": "var o3=Object.create;function f(){return o3(null)}\n",
		"real.ts":   "const r = await client.responses.create({ model: \"o3\" });\n",
		"cfg.yaml":  "model: o3\n",
	})
	report, err := Analyze(dir, mustCatalog(t))
	if err != nil {
		t.Fatal(err)
	}
	byFile := map[string]int{}
	for _, s := range report.Sites {
		byFile[s.File]++
	}
	if byFile["bundle.js"] != 0 {
		t.Errorf("minified bundle produced %d o3 sites, want 0", byFile["bundle.js"])
	}
	if byFile["real.ts"] != 1 {
		t.Errorf("quoted o3 produced %d sites, want 1", byFile["real.ts"])
	}
	if byFile["cfg.yaml"] != 1 {
		t.Errorf("config bound o3 produced %d sites, want 1", byFile["cfg.yaml"])
	}
}

// A docstring is a comment in every way except syntactically, and was
// the one spelling of a comment layer 2 still read after comments were
// masked. The single line string must survive: a REST endpoint naming
// the model in its path is a real call.
func TestMultiLineStringsAreProse(t *testing.T) {
	dir := writeTree(t, map[string]string{
		"a.py": `"""Pipeline helper.

Historically this used claude-3-5-sonnet-20241022 before we moved on.
"""

def go():
    return 1
`,
		"b.cs": "class T { void R() {\n  var u = \"https://generativelanguage.googleapis.com/v1beta/models/gemini-2.5-flash:generateContent\";\n} }\n",
	})
	report, err := Analyze(dir, mustCatalog(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range report.Sites {
		if s.File == "a.py" {
			t.Errorf("docstring produced a site: %+v", s)
		}
	}
	found := false
	for _, s := range report.Sites {
		if s.File == "b.cs" && s.ModelID == "gemini-2.5-flash" {
			found = true
		}
	}
	if !found {
		t.Errorf("the REST endpoint model was lost: %+v", report.Sites)
	}
}

// A provider shipping a version the catalog has not caught up to must
// not vanish. humanasllm pins deepseek-v4-flash; the catalog carries
// only deepseek-chat and deepseek-reasoner, and the pattern had no
// deepseek branch at all, so the repository's only real call site
// produced nothing.
func TestUnknownFamiliesAreStillModelLooking(t *testing.T) {
	future := []string{
		"deepseek-v4-flash", "qwen-3.5-max", "grok-5-fast",
		"glm-6", "kimi-k3", "gpt-5.4-mini", "codestral-26",
	}
	for _, id := range future {
		dir := writeTree(t, map[string]string{"a.js": "const M = \"" + id + "\";\n"})
		report, err := Analyze(dir, mustCatalog(t))
		if err != nil {
			t.Fatal(err)
		}
		if len(report.Sites) != 1 || report.Sites[0].Known {
			t.Errorf("%q gave %+v, want exactly one unknown site", id, report.Sites)
		}
	}

	// The families left out on purpose, and the shapes the boundary
	// guard covers. Widening the pattern must not cost precision.
	notModels := []string{
		"nova-scotia-parser", "embed-the-widget", "novalidate",
		"my-command-runner", "deepseeking-truth", "grokking-tests",
	}
	for _, s := range notModels {
		dir := writeTree(t, map[string]string{"a.js": "const M = \"" + s + "\";\n"})
		report, err := Analyze(dir, mustCatalog(t))
		if err != nil {
			t.Fatal(err)
		}
		if len(report.Sites) != 0 {
			t.Errorf("%q produced %d sites, want none: %+v", s, len(report.Sites), report.Sites)
		}
	}
}

// Agent tooling borrows the family name of the model it drives. demarkus
// is an MCP broker with no LLM dependency and produced 37 sites, all of
// them plugin manifests and hook scripts naming claude-code.
func TestAgentToolingIsNotAModel(t *testing.T) {
	tools := []string{
		"claude-code", "claude-code-knowledge", "claude-templates",
		"claude-plugin", "claude-pre", "claude-post",
		"gemini-cli", "gemini-hook", "gpt-cli", "claude-mcp",
	}
	for _, name := range tools {
		dir := writeTree(t, map[string]string{"a.js": "const t = \"" + name + "\";\n"})
		report, err := Analyze(dir, mustCatalog(t))
		if err != nil {
			t.Fatal(err)
		}
		if len(report.Sites) != 0 {
			t.Errorf("%q produced %d sites, want none: %+v", name, len(report.Sites), report.Sites)
		}
	}

	// The filter keys on the word after the family, so a real model
	// whose name merely starts the same way must survive.
	for _, id := range []string{"claude-opus-5", "gemini-3-flash-preview", "gpt-5.4-mini"} {
		dir := writeTree(t, map[string]string{"a.js": "const m = \"" + id + "\";\n"})
		report, err := Analyze(dir, mustCatalog(t))
		if err != nil {
			t.Fatal(err)
		}
		if len(report.Sites) != 1 {
			t.Errorf("%q gave %d sites, want 1", id, len(report.Sites))
		}
	}
}
