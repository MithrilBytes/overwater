package scan

import (
	"strings"
	"testing"
)

// The baseline fingerprint is the call's structure. Rewording a prompt
// of any length or moving the call must not read as a new finding;
// changing a parameter or the model must. The legacy hash is what
// older baselines were recorded under, and it did churn on a short
// prompt, which is why both are kept until those files are re-recorded.
func TestSiteHashReadsStructureNotWording(t *testing.T) {
	base := `import anthropic

client = anthropic.Anthropic()


def triage(text):
    return client.messages.create(
        model="claude-opus-5",
        max_tokens=300,
        system="Be brief.",
        messages=[{"role": "user", "content": text}],
    )
`
	hashes := func(src string) (string, string) {
		t.Helper()
		r := analyzeTemp(t, map[string]string{"app.py": src})
		if len(r.Sites) != 1 {
			t.Fatalf("sites = %d, want 1", len(r.Sites))
		}
		return r.Sites[0].Hash, r.Sites[0].HashLegacy
	}
	want, legacy := hashes(base)
	if want == "" || legacy == "" {
		t.Fatal("empty hash")
	}
	reworded := strings.Replace(base, `"Be brief."`, `"Answer briefly."`, 1)
	same := map[string]string{
		"short prompt reworded": reworded,
		"long prompt reworded": strings.Replace(base, `"Be brief."`,
			`"Answer in one short paragraph and name the ticket, the customer and the next step, nothing else."`, 1),
		"call moved down": "\n\n\n" + base,
	}
	for name, src := range same {
		if got, _ := hashes(src); got != want {
			t.Errorf("%s: hash changed", name)
		}
	}
	different := map[string]string{
		"max_tokens changed": strings.Replace(base, "max_tokens=300", "max_tokens=400", 1),
		"model changed":      strings.Replace(base, "claude-opus-5", "claude-sonnet-5", 1),
	}
	for name, src := range different {
		if got, _ := hashes(src); got == want {
			t.Errorf("%s: hash did not change", name)
		}
	}
	if _, got := hashes(reworded); got == legacy {
		t.Error("the legacy hash ignored the short prompt; it never did, and older baselines depend on that")
	}
	if got, _ := hashes(strings.Replace(base, `"content": text`, `"text": text`, 1)); got == want {
		t.Error("renaming a key did not change the hash")
	}
}
