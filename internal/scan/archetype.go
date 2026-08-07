package scan

import (
	"regexp"
	"strings"
)

// Archetypes layer 4 can assign to a call site.
const (
	ArchetypeEmbedding      = "embedding"
	ArchetypeClassification = "classification"
	ArchetypeExtraction     = "extraction"
	ArchetypeSummarization  = "summarization"
	ArchetypeAgentic        = "agentic"
	ArchetypeChat           = "chat"
	ArchetypeTranslation    = "translation"
	ArchetypeReranking      = "reranking"
	ArchetypeModeration     = "moderation"
	ArchetypeTranscription  = "transcription"
	ArchetypeVision         = "vision"
	ArchetypeCodegen        = "codegen"
	ArchetypeUnknown        = "unknown"
)

// archetypePriority breaks score ties deterministically; the narrower
// task classes come before the broad ones.
var archetypePriority = []string{
	ArchetypeModeration,
	ArchetypeReranking,
	ArchetypeTranslation,
	ArchetypeTranscription,
	ArchetypeVision,
	ArchetypeClassification,
	ArchetypeExtraction,
	ArchetypeSummarization,
	ArchetypeCodegen,
	ArchetypeAgentic,
	ArchetypeChat,
}

// codeWords are matched against identifiers near the call (the function
// name, the code in the region); promptWords against the resolved system
// prompt, where a conversational word like "assistant" is evidence
// rather than noise.
type archetypeKeywords struct {
	archetype   string
	codeWords   []string
	promptWords []string
}

var archetypeWords = []archetypeKeywords{
	{ArchetypeClassification,
		[]string{"classif", "categor", "triage", "sentiment"},
		[]string{"classif", "categor", "triage", "sentiment"}},
	{ArchetypeExtraction,
		[]string{"extract", "parse", "invoice", "receipt"},
		[]string{"extract", "parse", "invoice", "receipt"}},
	{ArchetypeSummarization,
		[]string{"summar", "digest", "recap"},
		[]string{"summar", "digest", "recap"}},
	{ArchetypeChat,
		[]string{"chat"},
		[]string{"chat", "conversation", "assistant"}},
	{ArchetypeTranslation,
		[]string{"translat", "localiz"},
		[]string{"translat", "target language"}},
	{ArchetypeReranking,
		[]string{"rerank"},
		[]string{"rerank", "order by relevance", "most relevant"}},
	{ArchetypeModeration,
		[]string{"moderat", "safety_gate", "content_filter"},
		[]string{"moderat", "allow or block", "policy violation"}},
	// Words that mean the transcription task itself; the bare stem
	// transcri would also match transcripts, which usually names the
	// input of a summarizer, not this task.
	{ArchetypeTranscription,
		[]string{"transcrib", "transcription", "whisper", "speech_to_text"},
		[]string{"transcrib", "transcription", "speech to text", "word for word", "verbatim"}},
	{ArchetypeVision,
		[]string{"vision", "ocr", "screenshot"},
		[]string{"ocr", "in the image", "in this image"}},
	{ArchetypeCodegen,
		[]string{"codegen", "write_code", "generate_code", "writecode", "generatecode", "autocomplete"},
		[]string{"write code", "generate code", "unit test", "sql"}},
}

// Endpoint names, which are stronger evidence than any keyword.
var calleeSignals = []struct {
	marker    string
	archetype string
}{
	{"moderations", ArchetypeModeration},
	{"transcription", ArchetypeTranscription},
	{"transcribe", ArchetypeTranscription},
	{".rerank", ArchetypeReranking},
	{"fim.complete", ArchetypeCodegen},
	{"image_url", ArchetypeVision},
	{"inline_data", ArchetypeVision},
	{"inlinedata", ArchetypeVision},
}

// The enclosing function name is the strongest single signal, the
// prompt states the task in its own words, and stray code identifiers
// count least.
const (
	weightFuncName = 5
	weightPrompt   = 3
	weightCode     = 2
)

// An archetype pragma pins a call site the heuristics get wrong:
//
//	// overwater:archetype=extraction
var rePragma = regexp.MustCompile(`overwater:archetype=([a-z]+)`)

func validArchetype(s string) bool {
	switch s {
	case ArchetypeEmbedding, ArchetypeClassification, ArchetypeExtraction,
		ArchetypeSummarization, ArchetypeAgentic, ArchetypeChat,
		ArchetypeTranslation, ArchetypeReranking, ArchetypeModeration,
		ArchetypeTranscription, ArchetypeVision, ArchetypeCodegen:
		return true
	}
	return false
}

// classify returns the archetype the call site's evidence favours, with
// a graded confidence. A pragma in or just above the region, and an
// embedding call, both win outright; everything else is scored.
func (a *analyzer) classify(p string, shape Shape, r region, tier string) (string, string) {
	content := a.byPath[p]
	pragmaStart := linesAbove(content, r.start, 3)
	if m := rePragma.FindStringSubmatch(content[pragmaStart:r.end]); m != nil && validArchetype(m[1]) {
		return m[1], "high"
	}
	if shape.EmbeddingCall || tier == "embedding" {
		return ArchetypeEmbedding, "high"
	}
	return rank(a.archetypeScores(p, shape, r))
}

// archetypeScores weighs every archetype against the evidence around the
// call: the enclosing function name, the code in the region, the
// resolved system prompt, and the shape itself.
func (a *analyzer) archetypeScores(p string, shape Shape, r region) map[string]int {
	src := a.masked(p)
	// Keyword evidence in code comes from identifiers alone: the fully
	// masked view drops every string, so a short prose fragment like
	// "Triage notes: " cannot pose as code.
	code := strings.ToLower(src.all[r.start:r.end])
	// The prompt's content already scores through promptWords; counting
	// the name of the variable that holds it would count the same
	// evidence twice.
	for _, ident := range promptIdents(src.prose[r.start:r.end]) {
		code = strings.ReplaceAll(code, strings.ToLower(ident), " ")
	}
	// The enclosing function is the last definition before the model
	// string, not before the head expanded region start.
	funcName := strings.ToLower(enclosingFuncName(src.prose, r.hit))
	prompt := strings.ToLower(shape.SystemPromptText)

	scores := map[string]int{}
	for _, fam := range archetypeWords {
		if containsAny(funcName, fam.codeWords) {
			scores[fam.archetype] += weightFuncName
		}
		if containsAny(code, fam.codeWords) {
			scores[fam.archetype] += weightCode
		}
		if prompt != "" && containsAny(prompt, fam.promptWords) {
			scores[fam.archetype] += weightPrompt
		}
	}
	for _, sig := range calleeSignals {
		if strings.Contains(code, sig.marker) {
			scores[sig.archetype] += 4
		}
	}
	if shape.ImageDetailHigh {
		scores[ArchetypeVision] += 2
	}
	if shape.SchemaEnumOnly {
		scores[ArchetypeClassification] += 4
	}
	if shape.SchemaMultiField {
		scores[ArchetypeExtraction] += 3
	}
	if shape.JSONSchema {
		scores[ArchetypeExtraction]++
	}
	if shape.ForcedTool {
		scores[ArchetypeExtraction] += 2
	}
	if shape.Tools && !shape.ForcedTool {
		scores[ArchetypeAgentic] += 2
	}
	if shape.Streaming {
		scores[ArchetypeChat] += 2
	}
	return scores
}

// rank returns the highest scoring archetype and how sure of it we are.
// A win needs both an absolute score and a margin over the runner up;
// ties fall to archetypePriority, which is ordered narrowest first.
func rank(scores map[string]int) (string, string) {
	best, bestScore, secondScore := "", 0, 0
	for _, arch := range archetypePriority {
		s := scores[arch]
		if s > bestScore {
			best, bestScore, secondScore = arch, s, bestScore
		} else if s > secondScore {
			secondScore = s
		}
	}
	if bestScore == 0 {
		return ArchetypeUnknown, "low"
	}
	margin := bestScore - secondScore
	switch {
	case bestScore >= 4 && margin >= 3:
		return best, "high"
	case bestScore <= 2 || margin <= 1:
		return best, "low"
	default:
		return best, "medium"
	}
}

// promptIdents lists identifiers the region assigns as its system
// prompt, in any of the recognized forms.
func promptIdents(region string) []string {
	var names []string
	for _, re := range []*regexp.Regexp{reSystemIdent, reRoleSystem, reTextField} {
		for _, m := range re.FindAllStringSubmatch(region, -1) {
			if v := m[1]; len(v) > 1 {
				names = append(names, v)
			}
		}
	}
	return names
}

// linesAbove returns the offset n line starts above from, bounded by
// lookbackMaxBytes for the same reason headExpand is.
func linesAbove(content string, from, n int) int {
	limit := max(0, from-lookbackMaxBytes)
	i := from
	for k := 0; k <= n; k++ {
		nl := strings.LastIndexByte(content[limit:i], '\n')
		if nl < 0 {
			return limit
		}
		i = limit + nl
	}
	return i + 1
}

func containsAny(s string, words []string) bool {
	for _, w := range words {
		if strings.Contains(s, w) {
			return true
		}
	}
	return false
}
