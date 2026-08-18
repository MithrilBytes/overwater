package scan

import "testing"

// Bedrock Converse has no flat spelling: maxTokens and temperature are
// legal only inside inferenceConfig, which the property reader does not
// look into. The structural parse must not erase what the regex layer
// already read there, or the call prices at the default output length
// instead of its own cap.
func TestConverseInferenceConfigKeepsRegexShape(t *testing.T) {
	r := analyzeTemp(t, map[string]string{"converse.py": `import boto3

bedrock = boto3.client("bedrock-runtime")


def triage_ticket(text):
    return bedrock.converse(
        modelId="anthropic.claude-opus-4-5-20251101-v1:0",
        messages=[{"role": "user", "content": [{"text": text}]}],
        inferenceConfig={"maxTokens": 16, "temperature": 0},
    )
`})
	s := soleSite(t, r).Shape
	if s.MaxTokens == nil || *s.MaxTokens != 16 {
		t.Errorf("max tokens = %v, want 16; the parse erased the regex read cap", s.MaxTokens)
	}
	if s.Temperature == nil || *s.Temperature != 0 {
		t.Errorf("temperature = %v, want 0; the parse erased the regex read value", s.Temperature)
	}
}

// A parser that reads a property still overrules the regexes: the
// nesting the reader does understand has the final word.
func TestConfigWrapperStillOverrulesRegex(t *testing.T) {
	r := analyzeTemp(t, map[string]string{"draft.py": `from google import genai


def draft_copy(topic):
    return client.models.generate_content(
        model="gemini-2.5-flash",
        contents=topic,
        config={"temperature": 0.2},
    )
`})
	s := soleSite(t, r).Shape
	if s.Temperature == nil || *s.Temperature != 0.2 {
		t.Errorf("temperature = %v, want 0.2 from the config wrapper", s.Temperature)
	}
}

// Kebab case is the canonical Spring AI spelling
// (spring.ai.openai.chat.options.max-tokens), so a hyphenated key has
// to fold onto the same name as the underscored one.
func TestNormKeyFoldsHyphens(t *testing.T) {
	if got, want := normKey("max-tokens"), normKey("max_tokens"); got != want {
		t.Errorf("normKey(max-tokens) = %q, want %q as max_tokens folds", got, want)
	}
	if got := normKey("top-P"); got != "topp" {
		t.Errorf("normKey(top-P) = %q, want topp", got)
	}
}
