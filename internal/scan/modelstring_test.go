package scan

import "testing"

// AWS cross region inference profiles put a region in front of the
// vendor, and the catalog cannot carry an alias per region per model.
// The fallback pattern is what covers them; the dot to its left used to
// kill it, so a Bedrock repository reported nothing at all.
func TestBedrockCrossRegionProfilesReport(t *testing.T) {
	cases := []struct {
		file, id, want string
	}{
		{"apac.py", "apac.anthropic.claude-opus-4-9-20260601-v1:0", "claude-opus-4-9-20260601-v1"},
		{"global.py", "global.anthropic.claude-opus-4-5-20251101-v1:0", "claude-opus-4-5-20251101-v1"},
		// A revision bump is one character off the alias list.
		{"rev.py", "us.anthropic.claude-opus-4-5-20251101-v2:0", "claude-opus-4-5-20251101-v2"},
	}
	for _, c := range cases {
		r := analyzeTemp(t, map[string]string{c.file: `import boto3


def triage_ticket(text):
    return boto3.client("bedrock-runtime").converse(
        modelId="` + c.id + `",
        messages=[{"role": "user", "content": [{"text": text}]}],
    )
`})
		site := soleSite(t, r)
		if site.Known {
			t.Errorf("%s: %s read as catalogued, want the unlisted fallback", c.file, c.id)
		}
		if site.Ref != c.want {
			t.Errorf("%s: ref = %q, want %q", c.file, site.Ref, c.want)
		}
	}
}

// The dot stays a name character on the catalog path: the hand written
// Bedrock aliases carry dots and have to match as whole keys, priced,
// rather than falling through to the unlisted fallback.
func TestBedrockAliasStaysCatalogued(t *testing.T) {
	r := analyzeTemp(t, map[string]string{"exact.py": `import boto3


def triage_ticket(text):
    return boto3.client("bedrock-runtime").converse(
        modelId="us.anthropic.claude-opus-4-5-20251101-v1:0",
        messages=[{"role": "user", "content": [{"text": text}]}],
    )
`})
	site := soleSite(t, r)
	if !site.Known || site.ModelID != "claude-opus-4-5" {
		t.Errorf("aliased id = %q known=%v, want claude-opus-4-5 priced", site.ModelID, site.Known)
	}
}

// Agent tooling reached through a dotted config path is still not a
// model: relaxing the left edge must not turn every plugin key into a
// call site.
func TestDottedToolNamesStayOut(t *testing.T) {
	r := analyzeTemp(t, map[string]string{"hooks.js": `const cfg = {
  "plugins.claude-code.hooks": ["pre"],
  "plugins.gemini-cli.hooks": ["post"],
};
`})
	if len(r.Sites) != 0 {
		t.Errorf("got %d sites, want none: %+v", len(r.Sites), r.Sites)
	}
}
