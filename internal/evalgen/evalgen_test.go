package evalgen

import (
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/MithrilBytes/overwater/catalog"
	"github.com/MithrilBytes/overwater/rules"
)

// testCatalog holds a chat pair per provider and an embedding pair,
// priced so the filled numbers are easy to spot in a script body.
func testCatalog() *catalog.Catalog {
	providers := []string{
		"anthropic", "openai", "deepseek", "xai",
		"groq", "mistral", "google", "cohere", "alibaba",
	}
	var models []catalog.Model
	for _, p := range providers {
		models = append(models,
			catalog.Model{ID: p + "-big", Provider: p, Tier: "frontier", InputPerMtok: 2.5, OutputPerMtok: 10},
			catalog.Model{ID: p + "-small", Provider: p, Tier: "small", InputPerMtok: 0.25, OutputPerMtok: 1},
		)
	}
	models = append(models,
		catalog.Model{ID: "embed-big", Provider: "openai", Tier: "embedding", InputPerMtok: 0.13},
		catalog.Model{ID: "embed-small", Provider: "openai", Tier: "embedding", InputPerMtok: 0.02},
		catalog.Model{ID: "embed-foreign", Provider: "voyage", Tier: "embedding", InputPerMtok: 0.1},
	)
	return &catalog.Catalog{Version: "2026-08-01", Models: models}
}

func chatFinding(provider string) rules.Finding {
	return rules.Finding{
		RuleID:         "tier-downgrade",
		File:           "app/main.py",
		Line:           12,
		Model:          provider + "-big",
		CandidateModel: provider + "-small",
		Tripwire:       "If eval agreement drops below 97%, stay put.",
	}
}

// generateOne runs Generate for a single finding, expects one script,
// syntax checks it, and returns its body.
func generateOne(t *testing.T, f rules.Finding) string {
	t.Helper()
	dir := t.TempDir()
	written, skipped, err := Generate([]rules.Finding{f}, testCatalog(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(written) != 1 || len(skipped) != 0 {
		t.Fatalf("written = %v, skipped = %v, want one script", written, skipped)
	}
	body, err := os.ReadFile(written[0])
	if err != nil {
		t.Fatal(err)
	}
	pyCompile(t, written[0])
	return string(body)
}

// pyCompile syntax checks a generated script when python3 is around.
func pyCompile(t *testing.T, path string) {
	t.Helper()
	py, err := exec.LookPath("python3")
	if err != nil {
		t.Log("python3 not on PATH; skipping compile check")
		return
	}
	out, err := exec.Command(py, "-m", "py_compile", path).CombinedOutput()
	if err != nil {
		t.Errorf("generated script does not compile: %v\n%s", err, out)
	}
}

func TestGenerateChatScripts(t *testing.T) {
	cases := []struct {
		provider string
		wants    []string
	}{
		{"anthropic", []string{"import anthropic", "ANTHROPIC_API_KEY"}},
		{"openai", []string{"from openai import OpenAI", "OPENAI_API_KEY"}},
		{"deepseek", []string{"from openai import OpenAI", `base_url="https://api.deepseek.com"`, "DEEPSEEK_API_KEY"}},
		{"xai", []string{`base_url="https://api.x.ai/v1"`, "XAI_API_KEY"}},
		{"groq", []string{`base_url="https://api.groq.com/openai/v1"`, "GROQ_API_KEY"}},
		{"mistral", []string{`base_url="https://api.mistral.ai/v1"`, "MISTRAL_API_KEY"}},
		{"google", []string{"from google import genai", "genai.Client()", "GEMINI_API_KEY"}},
		{"cohere", []string{"import cohere", "cohere.ClientV2", "COHERE_API_KEY"}},
	}
	for _, tc := range cases {
		t.Run(tc.provider, func(t *testing.T) {
			body := generateOne(t, chatFinding(tc.provider))
			wants := append([]string{
				`CURRENT = "` + tc.provider + `-big"`,
				`CANDIDATE = "` + tc.provider + `-small"`,
				"CURRENT_IN = 2.5\n",
				"CURRENT_OUT = 10\n",
				"CANDIDATE_IN = 0.25\n",
				"CANDIDATE_OUT = 1\n",
			}, tc.wants...)
			for _, want := range wants {
				if !strings.Contains(body, want) {
					t.Errorf("script is missing %q", want)
				}
			}
			if strings.Contains(body, "{{") {
				t.Errorf("script still has an unfilled placeholder")
			}
		})
	}
}

func TestGenerateEmbeddingScript(t *testing.T) {
	f := rules.Finding{
		RuleID:         "cheaper-embedding",
		File:           "rag/index.py",
		Line:           7,
		Model:          "embed-big",
		CandidateModel: "embed-small",
		Tripwire:       "If recall drops, stay put.",
	}
	body := generateOne(t, f)
	for _, want := range []string{
		`CURRENT = "embed-big"`,
		`CANDIDATE = "embed-small"`,
		"nearest neighbor agreement",
		"recall at 3",
		"pairs.jsonl",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("script is missing %q", want)
		}
	}
	if strings.Contains(body, "{{") {
		t.Errorf("script still has an unfilled placeholder")
	}
}

func TestGenerateSkips(t *testing.T) {
	cases := []struct {
		name      string
		model     string
		candidate string
		reason    string
	}{
		{"chat provider without a template", "alibaba-big", "alibaba-small", "no eval template for provider alibaba yet"},
		{"embedding provider without a template", "embed-foreign", "embed-small", "no embedding eval template for provider voyage yet"},
		{"model outside the catalog", "mystery-9000", "embed-small", "model is not in the catalog"},
		{"candidate is not a different model", "anthropic-big", "", "candidate is not a different model"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := chatFinding("anthropic")
			f.Model = tc.model
			f.CandidateModel = tc.candidate
			dir := t.TempDir()
			written, skipped, err := Generate([]rules.Finding{f}, testCatalog(), dir)
			if err != nil {
				t.Fatal(err)
			}
			if len(written) != 0 {
				t.Fatalf("written = %v, want none", written)
			}
			if len(skipped) != 1 || !strings.Contains(skipped[0], tc.reason) {
				t.Errorf("skipped = %v, want reason %q", skipped, tc.reason)
			}
		})
	}
}
