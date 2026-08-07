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
