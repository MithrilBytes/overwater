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
	ArchetypeUnknown        = "unknown"
)

// archetypePriority breaks score ties deterministically.
var archetypePriority = []string{
	ArchetypeClassification,
	ArchetypeExtraction,
	ArchetypeSummarization,
	ArchetypeAgentic,
	ArchetypeChat,
}

// A family's codeWords are matched against identifiers near the call
// (function name, masked code in the extent); promptWords are matched
// against the resolved system prompt, where conversational words like
// "assistant" are evidence rather than noise.
type family struct {
	archetype   string
	codeWords   []string
	promptWords []string
}

var families = []family{
	{ArchetypeClassification,
		[]string{"classif", "categor", "triage", "sentiment", "moderat"},
		[]string{"classif", "categor", "triage", "sentiment", "moderat"}},
	{ArchetypeExtraction,
		[]string{"extract", "parse", "invoice", "receipt"},
		[]string{"extract", "parse", "invoice", "receipt"}},
	{ArchetypeSummarization,
		[]string{"summar", "digest", "recap"},
		[]string{"summar", "digest", "recap"}},
	{ArchetypeChat,
		[]string{"chat"},
		[]string{"chat", "conversation", "assistant"}},
}

// Signal weights: the enclosing function name is the strongest single
// signal, the prompt states the task in its own words, and stray code
// identifiers count least.
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
		ArchetypeSummarization, ArchetypeAgentic, ArchetypeChat:
		return true
	}
	return false
}

// classify scores every archetype from the call site's own evidence and
// returns the winner with a graded confidence. A pragma in or just
// above the region wins outright. hit anchors the enclosing function
// lookup: the last definition before the model string is the function
// the call lives in, which the head expanded region start is not.
func (a *analyzer) classify(p string, shape Shape, regionStart, regionEnd, hit int, tier string) (string, string) {
	content := a.byPath[p]
	pragmaStart := linesAbove(content, regionStart, 3)
	if m := rePragma.FindStringSubmatch(content[pragmaStart:regionEnd]); m != nil && validArchetype(m[1]) {
		return m[1], "high"
	}
	if shape.EmbeddingCall || tier == "embedding" {
		return ArchetypeEmbedding, "high"
	}

	m := a.masked(p)
	// Keyword evidence in code comes from identifiers alone: the fully
	// masked view drops every string, so a short prose fragment like
	// "Triage notes: " cannot pose as code.
	code := strings.ToLower(m.all[regionStart:regionEnd])
	// The prompt's content already scores through promptWords; counting
	// the name of the variable that holds it would count the same
	// evidence twice.
	for _, ident := range promptIdents(m.prose[regionStart:regionEnd]) {
		code = strings.ReplaceAll(code, strings.ToLower(ident), " ")
	}
	funcName := strings.ToLower(enclosingFuncName(m.prose, hit))
	prompt := strings.ToLower(shape.SystemPromptText)

	scores := map[string]int{}
	for _, fam := range families {
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

// linesAbove returns the offset n line starts above from.
func linesAbove(content string, from, n int) int {
	i := from
	for k := 0; k <= n; k++ {
		nl := strings.LastIndexByte(content[:i], '\n')
		if nl < 0 {
			return 0
		}
		i = nl
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
