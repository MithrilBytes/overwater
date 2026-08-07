package scan

import (
	"strings"
	"testing"
)

// Python spells booleans capitalized; stream=True must read as
// streaming instead of the structural layer overriding the regex layer
// that had it right.
func TestStreamTrueFromPythonKwarg(t *testing.T) {
	r := analyzeTemp(t, map[string]string{"chat.py": `def chat_reply(text):
    return client.chat.completions.create(
        model="gpt-5.1",
        stream=True,
        messages=[{"role": "user", "content": text}],
    )
`})
	if s := soleSite(t, r).Shape; !s.Streaming {
		t.Error("stream=True read as not streaming")
	}
}

// Numeric literals with underscore digit separators must parse whole.
func TestMaxTokensUnderscoreSeparators(t *testing.T) {
	r := analyzeTemp(t, map[string]string{"big.py": `def summarize_book(text):
    return client.messages.create(
        model="claude-sonnet-5",
        max_tokens=32_768,
        messages=[{"role": "user", "content": text}],
    )
`})
	s := soleSite(t, r).Shape
	if s.MaxTokens == nil || *s.MaxTokens != 32768 {
		t.Errorf("max tokens = %v, want 32768 with underscore separators", s.MaxTokens)
	}
}

// Google GenAI's typed config is a constructor call, which is the form
// the current docs use. Its keyword arguments are the call's parameters
// and must read the same as the dict spelling, which only descended into
// values that started with a brace.
func TestTypedConfigWrapperReadsLikeDict(t *testing.T) {
	const typed = `from google import genai
from google.genai import types


def draft_copy(topic):
    return client.models.generate_content(
        model="gemini-2.5-flash",
        contents=topic,
        config=types.GenerateContentConfig(
            system_instruction="You draft short marketing copy.",
            temperature=0.2,
        ),
    )
`
	const dict = `from google import genai


def draft_copy(topic):
    return client.models.generate_content(
        model="gemini-2.5-flash",
        contents=topic,
        config={
            "system_instruction": "You draft short marketing copy.",
            "temperature": 0.2,
        },
    )
`
	control := soleSite(t, analyzeTemp(t, map[string]string{"draft.py": dict})).Shape
	if control.Temperature == nil || *control.Temperature != 0.2 {
		t.Fatalf("control dict form read temperature %v, want 0.2", control.Temperature)
	}
	s := soleSite(t, analyzeTemp(t, map[string]string{"draft.py": typed})).Shape
	if s.Temperature == nil || *s.Temperature != *control.Temperature {
		t.Errorf("typed config read temperature %v, want %v as the dict form does",
			s.Temperature, control.Temperature)
	}
	if s.MaxTokens != nil {
		t.Errorf("typed config invented max tokens %v", *s.MaxTokens)
	}
}

// The descent reaches into a bare constructor and a package qualified
// one, and leaves values that are neither alone.
func TestWrapperPropsAcceptsConstructorCalls(t *testing.T) {
	for _, tc := range []struct {
		name, value, want string
	}{
		{"object literal", `{ "temperature": 0.4 }`, "0.4"},
		{"bare constructor", `GenerateContentConfig(temperature=0.4)`, "0.4"},
		{"qualified constructor", `types.GenerateContentConfig(temperature=0.4)`, "0.4"},
		{"nested qualifier", `genai.types.GenerateContentConfig(temperature = 0.4)`, "0.4"},
	} {
		props := wrapperProps(tc.value)
		if props == nil {
			t.Errorf("%s: no properties read from %s", tc.name, tc.value)
			continue
		}
		if got := props["temperature"]; got != tc.want {
			t.Errorf("%s: temperature = %q, want %q", tc.name, got, tc.want)
		}
	}
	for _, value := range []string{"CONFIG", `"config.json"`, "", "42"} {
		if props := wrapperProps(value); props != nil {
			t.Errorf("%q read as a wrapper: %v", value, props)
		}
	}
}

// Resolving PROMPT must not match the tail of LEGACY_PROMPT.
func TestResolveConstNameBoundary(t *testing.T) {
	content := "const LEGACY_PROMPT = `retired words nobody wants`;\nconst PROMPT = `current words`;\n"
	text, ok := resolveConstIn(content, "PROMPT")
	if !ok || text != "current words" {
		t.Errorf("resolved %q %v, want the PROMPT constant, not the LEGACY_PROMPT tail", text, ok)
	}
	// A name at the very start of the content still resolves.
	head := "HEAD = \"first\"\n"
	if text, ok := resolveConstIn(head, "HEAD"); !ok || text != "first" {
		t.Errorf("resolved %q %v, want first at start of content", text, ok)
	}
}

// An escaped quote inside a system prompt is part of the prompt, not
// its closing delimiter.
func TestSystemPromptKeepsEscapedQuotes(t *testing.T) {
	r := analyzeTemp(t, map[string]string{"greet.py": `def greet_user(name):
    return client.messages.create(
        model="claude-haiku-4-5",
        max_tokens=100,
        system="Say \"hello\" and use the preferred salutation of the person.",
        messages=[{"role": "user", "content": name}],
    )
`})
	s := soleSite(t, r).Shape
	want := `Say \"hello\" and use the preferred salutation of the person.`
	if s.SystemPromptText != want {
		t.Errorf("system prompt = %q, want the full literal past the escaped quotes", s.SystemPromptText)
	}
}

// "undef describeThing" must not pose as a function definition.
func TestEnclosingFuncNameNeedsWordBoundary(t *testing.T) {
	prose := "function summarizeAll(items) {\n  markUndef(undef describeThing);\n  const r = create({ model: X });\n}\n"
	hit := strings.Index(prose, "X")
	if name := enclosingFuncName(prose, hit); name != "summarizeAll" {
		t.Errorf("enclosing function = %q, want summarizeAll; undef posed as a definition", name)
	}
}

// The plural noun transcripts describes the input, not the task: a
// summarizer of transcripts is not transcription.
func TestTranscriptsPluralIsNotTranscription(t *testing.T) {
	r := analyzeTemp(t, map[string]string{"notes.py": `def prepare_notes(transcript):
    return client.messages.create(
        model="claude-sonnet-5",
        max_tokens=400,
        system="You summarize long meeting transcripts into decisions and action items.",
        messages=[{"role": "user", "content": transcript}],
    )
`})
	site := soleSite(t, r)
	if site.Archetype != ArchetypeSummarization {
		t.Errorf("archetype = %s (%s confidence), want summarization; transcripts is not the transcription task",
			site.Archetype, site.ArchetypeConfidence)
	}
}

// Task words that do mean transcription still classify as such.
func TestTranscribeWordingStaysTranscription(t *testing.T) {
	r := analyzeTemp(t, map[string]string{"audio.py": `def transcribe_voicemail(audio_b64):
    return client.messages.create(
        model="gemini-2.5-flash",
        max_tokens=800,
        system="Transcribe the audio word for word with speaker labels.",
        messages=[{"role": "user", "content": audio_b64}],
    )
`})
	site := soleSite(t, r)
	if site.Archetype != ArchetypeTranscription {
		t.Errorf("archetype = %s, want transcription from transcribe wording", site.Archetype)
	}
}
