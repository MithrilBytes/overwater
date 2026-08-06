package scan

import "testing"

// The call shape signals behind the effort, retry, dimensions, and
// image detail rules, across the regex and structural extraction paths.

func TestEffortFromTopLevelProp(t *testing.T) {
	r := analyzeTemp(t, map[string]string{"extract.ts": `async function extractInvoice(text: string) {
  return client.responses.create({
    model: "gpt-5.1",
    reasoning_effort: "high",
    max_tokens: 400,
  });
}
`})
	s := soleSite(t, r).Shape
	if s.Effort != "high" {
		t.Errorf("effort = %q, want high", s.Effort)
	}
}

func TestEffortFromNestedReasoningObject(t *testing.T) {
	r := analyzeTemp(t, map[string]string{"triage.ts": `async function triageTicket(text: string) {
  return client.responses.create({
    model: "gpt-5.1",
    reasoning: { effort: "xhigh" },
    max_tokens: 200,
  });
}
`})
	s := soleSite(t, r).Shape
	if s.Effort != "xhigh" {
		t.Errorf("effort = %q, want xhigh from the nested reasoning object", s.Effort)
	}
}

func TestEffortFromPythonKwarg(t *testing.T) {
	r := analyzeTemp(t, map[string]string{"label.py": `def label_row(text):
    return client.responses.create(
        model="o3",
        reasoning_effort="low",
        max_output_tokens=64,
    )
`})
	s := soleSite(t, r).Shape
	if s.Effort != "low" {
		t.Errorf("effort = %q, want low", s.Effort)
	}
}

func TestEffortAbsentStaysEmpty(t *testing.T) {
	r := analyzeTemp(t, map[string]string{"plain.ts": `async function ask(text: string) {
  return client.chat.completions.create({
    model: "gpt-5.1",
    max_tokens: 100,
  });
}
`})
	if s := soleSite(t, r).Shape; s.Effort != "" {
		t.Errorf("effort = %q, want empty when the call sets none", s.Effort)
	}
}

func TestRetriesFromCamelCaseProp(t *testing.T) {
	r := analyzeTemp(t, map[string]string{"agent.ts": `async function runAgent(task: string) {
  return client.messages.create({
    model: "claude-opus-5",
    maxRetries: 5,
    max_tokens: 1000,
  });
}
`})
	s := soleSite(t, r).Shape
	if s.MaxRetries == nil || *s.MaxRetries != 5 {
		t.Errorf("max retries = %v, want 5", s.MaxRetries)
	}
}

func TestRetriesFromBareAliasAndKwarg(t *testing.T) {
	r := analyzeTemp(t, map[string]string{"retry.py": `def call_model(text):
    return client.messages.create(
        model="claude-opus-5",
        retries=3,
        max_tokens=500,
    )
`})
	s := soleSite(t, r).Shape
	if s.MaxRetries == nil || *s.MaxRetries != 3 {
		t.Errorf("max retries = %v, want 3 from the bare retries kwarg", s.MaxRetries)
	}
}

func TestRetriesAbsentStaysNil(t *testing.T) {
	r := analyzeTemp(t, map[string]string{"once.ts": `async function once(text: string) {
  return client.messages.create({
    model: "claude-sonnet-5",
    max_tokens: 500,
  });
}
`})
	if s := soleSite(t, r).Shape; s.MaxRetries != nil {
		t.Errorf("max retries = %v, want nil when the call sets none", *s.MaxRetries)
	}
}

func TestDimensionsFromEmbeddingKwarg(t *testing.T) {
	r := analyzeTemp(t, map[string]string{"embed.py": `def embed_chunks(chunks):
    return client.embeddings.create(
        model="text-embedding-3-large",
        input=chunks,
        dimensions=512,
    )
`})
	s := soleSite(t, r).Shape
	if s.Dimensions == nil || *s.Dimensions != 512 {
		t.Errorf("dimensions = %v, want 512", s.Dimensions)
	}
}

func TestDimensionsAbsentStaysNil(t *testing.T) {
	r := analyzeTemp(t, map[string]string{"embed.py": `def embed_chunks(chunks):
    return client.embeddings.create(
        model="text-embedding-3-large",
        input=chunks,
    )
`})
	if s := soleSite(t, r).Shape; s.Dimensions != nil {
		t.Errorf("dimensions = %v, want nil when the call sets none", *s.Dimensions)
	}
}

func TestImageDetailHighInsideContentPart(t *testing.T) {
	r := analyzeTemp(t, map[string]string{"ocr.ts": `async function ocrReceipt(url: string) {
  return client.chat.completions.create({
    model: "gpt-4o",
    messages: [{ role: "user", content: [{ type: "image_url", image_url: { url, detail: "high" } }] }],
    max_tokens: 300,
  });
}
`})
	if s := soleSite(t, r).Shape; !s.ImageDetailHigh {
		t.Error("image detail high = false, want true from the nested content part")
	}
}

// The walker must skip the repo's own .overwater.yaml the same way it
// skips .overwater.json: config may name model ids and rule ids
// without becoming call sites.
func TestWalkSkipsOverwaterConfig(t *testing.T) {
	r := analyzeTemp(t, map[string]string{
		".overwater.yaml": "disable: [deprecated-model]\n# migrating off gpt-5.1\n",
	})
	if len(r.Sites) != 0 {
		t.Errorf("got %d sites from .overwater.yaml, want 0: %+v", len(r.Sites), r.Sites)
	}
}

func TestImageDetailLowStaysFalse(t *testing.T) {
	r := analyzeTemp(t, map[string]string{"ocr.ts": `async function ocrReceipt(url: string) {
  return client.chat.completions.create({
    model: "gpt-4o",
    messages: [{ role: "user", content: [{ type: "image_url", image_url: { url, detail: "low" } }] }],
    max_tokens: 300,
  });
}
`})
	if s := soleSite(t, r).Shape; s.ImageDetailHigh {
		t.Error("image detail high = true, want false for detail low")
	}
}
