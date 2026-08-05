package scan

import "strings"

// Archetypes layer 4 can assign to a call site.
const (
	ArchetypeEmbedding      = "embedding"
	ArchetypeClassification = "classification"
	ArchetypeExtraction     = "extraction"
	ArchetypeSummarization  = "summarization"
	ArchetypeAgentic        = "agentic"
	ArchetypeChat           = "chat"
	ArchetypeUnknown        = "unknown"
)

// Keyword families for the archetype heuristics. Matching is lowercase
// substring search over the same window layer 3 read.
var (
	classificationWords = []string{"classif", "categor", "triage", "sentiment", "moderat"}
	extractionWords     = []string{"extract", "parse", "invoice", "receipt"}
	summarizationWords  = []string{"summar", "digest", "recap"}
	chatWords           = []string{"chat", "conversation", "assistant"}
)

// classifyArchetype assigns the task class for one call site from its
// shape, the text around it, and the model's tier. A forced single tool
// choice reads as structured output, not an agentic loop.
func classifyArchetype(shape Shape, window, tier string) string {
	w := strings.ToLower(window)
	structured := shape.JSONSchema || shape.ForcedTool
	switch {
	case shape.EmbeddingCall || tier == "embedding":
		return ArchetypeEmbedding
	case containsAny(w, classificationWords):
		return ArchetypeClassification
	case structured || containsAny(w, extractionWords):
		return ArchetypeExtraction
	case containsAny(w, summarizationWords):
		return ArchetypeSummarization
	case shape.Tools && !shape.ForcedTool:
		return ArchetypeAgentic
	case shape.Streaming || containsAny(w, chatWords):
		return ArchetypeChat
	}
	return ArchetypeUnknown
}

func containsAny(s string, words []string) bool {
	for _, w := range words {
		if strings.Contains(s, w) {
			return true
		}
	}
	return false
}
