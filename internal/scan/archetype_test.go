package scan

import (
	"fmt"
	"strings"
	"testing"
)

// A prompt that rules a task out must not score as if it asked for it.
func TestNegatedPhrasesDoNotScore(t *testing.T) {
	cases := []struct {
		name   string
		prompt string
		want   bool
	}{
		{"plain", "Reply to the customer warmly.", true},
		{"never", "Never reply to the customer.", false},
		{"do not", "Do not reply to the customer.", false},
		{"contraction", "Don't reply to the customer.", false},
		{"avoid", "Avoid reply to the customer entirely.", false},
		// A negation binds to its own clause, not to the next one.
		{"previous sentence", "Never promise refunds. Reply to the customer warmly.", true},
		{"previous clause", "Avoid small talk; reply to the customer warmly.", true},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			got := saysAny(strings.ToLower(tt.prompt), []string{"reply to the customer"})
			if got != tt.want {
				t.Errorf("saysAny(%q) = %v, want %v", tt.prompt, got, tt.want)
			}
		})
	}
}

// A helper takes the prompt as an argument, so its own call site says
// nothing about the task and the scorer is left with a token cap and a
// temperature. Every caller says it outright, and layer 5 knows which
// calls those are.
func TestArchetypeCrossesTheFanInEdge(t *testing.T) {
	files := map[string]string{
		"llm.py": `import anthropic

client = anthropic.Anthropic()


def ow_complete(system, prompt, model="claude-opus-5", max_tokens=8, temperature=0):
    return client.messages.create(
        model=model,
        max_tokens=max_tokens,
        temperature=temperature,
        messages=[{"role": "system", "content": system},
                  {"role": "user", "content": prompt}],
    )
`,
	}
	for _, name := range []string{"a", "b", "c", "d"} {
		files[name+".py"] = fmt.Sprintf(`from llm import ow_complete


def route_%s(text):
    return ow_complete("Classify the ticket. Answer with one word: billing, bug, or other.", text)
`, name)
	}
	site := fanInSite(t, analyzeTemp(t, files), "llm.py")
	if site.FanInStatus != FanInExact || site.FanIn != 4 {
		t.Fatalf("fan in = %d (%s), want 4 exact", site.FanIn, site.FanInStatus)
	}
	if site.Archetype != ArchetypeClassification {
		t.Errorf("archetype = %s, want %s: the callers all ask for a label",
			site.Archetype, ArchetypeClassification)
	}
	// The callers' words describe this call by inheritance only.
	if site.ArchetypeConfidence == "high" {
		t.Errorf("archetype confidence = high, want no better than medium for second hand evidence")
	}
}

// A site that named its own task keeps its answer whatever its callers
// are called and whatever they pass.
func TestArchetypeCallersDoNotOverrideTheSite(t *testing.T) {
	files := map[string]string{
		"llm.py": `import anthropic

client = anthropic.Anthropic()


def summarize_release(prompt, model="claude-opus-5", max_tokens=800):
    return client.messages.create(
        model=model,
        max_tokens=max_tokens,
        messages=[{"role": "user", "content": prompt}],
    )
`,
	}
	for _, name := range []string{"a", "b", "c", "d"} {
		files[name+".py"] = fmt.Sprintf(`from llm import summarize_release


def classify_ticket_%s(text):
    return summarize_release("Classify the ticket. Answer with one word: billing, bug, or other.")
`, name)
	}
	site := fanInSite(t, analyzeTemp(t, files), "llm.py")
	if site.FanInStatus != FanInExact {
		t.Fatalf("fan in = %d (%s), want exact", site.FanIn, site.FanInStatus)
	}
	if site.Archetype != ArchetypeSummarization {
		t.Errorf("archetype = %s, want %s: the site named its own task",
			site.Archetype, ArchetypeSummarization)
	}
}
