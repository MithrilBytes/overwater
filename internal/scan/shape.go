package scan

import (
	"regexp"
	"strconv"
	"strings"
)

// windowLines bounds how far around a model reference layer 3 reads.
// These are per language heuristics over a text window, not parsing;
// when nothing readable is in the window the shape says so.
const windowLines = 30

var (
	reTemperature = regexp.MustCompile(`(?i)["']?temperature["']?\s*[:=]\s*([0-9]*\.?[0-9]+)`)
	reMaxTokens   = regexp.MustCompile(`(?i)["']?max_?(?:output_?)?tokens["']?\s*[:=]\s*([0-9][0-9_]*)`)
	reSchema      = regexp.MustCompile(`response_format|json_schema|input_schema|responseSchema|responseMimeType|generateObject`)
	reTools       = regexp.MustCompile(`(?i)["']?tools["']?\s*[:=]\s*\[`)
	reForcedTool  = regexp.MustCompile(`(?i)tool_?choice.{0,60}["']tool["']`)
	reStreaming   = regexp.MustCompile(`stream\s*[:=]\s*[Tt]rue|streamText\(|\.stream\(`)
	reCache       = regexp.MustCompile(`cache_control|cacheControl`)
	reEmbedding   = regexp.MustCompile(`embeddings\.create|embedContent|embed_content|\.embed\(`)
	reBatchAPI    = regexp.MustCompile(`batches\.create|/v1/batches|messages\.batches`)
	reBatchCtx    = regexp.MustCompile(`cron\.schedule|node-cron|crontab|schedule\.every|celery|BackgroundScheduler`)
	reCallish     = regexp.MustCompile(`\.create\(|generateText|generateObject|streamText|completions|embeddings|\.stream\(|messages\.create`)
)

func extractShape(data []byte, line int) Shape {
	content := string(data)
	starts := lineStarts(content)
	startLine := max(0, line-1-windowLines)
	endLine := min(len(starts), line+windowLines)
	winStart := starts[startLine]
	winEnd := len(content)
	if endLine < len(starts) {
		winEnd = starts[endLine]
	}
	window := content[winStart:winEnd]

	var s Shape
	s.BatchContext = reBatchCtx.MatchString(content)
	s.BatchAPI = reBatchAPI.MatchString(content)

	if m := reTemperature.FindStringSubmatch(window); m != nil {
		if v, err := strconv.ParseFloat(m[1], 64); err == nil {
			s.Temperature = &v
		}
	}
	if m := reMaxTokens.FindStringSubmatch(window); m != nil {
		if v, err := strconv.Atoi(strings.ReplaceAll(m[1], "_", "")); err == nil {
			s.MaxTokens = &v
		}
	}
	s.JSONSchema = reSchema.MatchString(window)
	s.Tools = reTools.MatchString(window)
	s.ForcedTool = reForcedTool.MatchString(window)
	s.Streaming = reStreaming.MatchString(window)
	s.CacheControl = reCache.MatchString(window)
	s.EmbeddingCall = reEmbedding.MatchString(window)
	s.SystemPromptChars = systemPromptChars(content, winStart, winEnd)
	s.Readable = reCallish.MatchString(window) ||
		s.Temperature != nil || s.MaxTokens != nil || s.JSONSchema || s.Streaming
	return s
}

func lineStarts(s string) []int {
	starts := []int{0}
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			starts = append(starts, i+1)
		}
	}
	return starts
}

// System prompt size heuristics, tried in order: an inline literal after
// system, an identifier after system resolved to a constant in the same
// file, a system content block list with a text field, and the chat
// completions style system role message. Anything else reports zero,
// which the rules treat as "not visible", never as "small".
var (
	reSystemInline = regexp.MustCompile("(?:\"system\"|system)\\s*[:=]\\s*([\"'`])")
	reSystemIdent  = regexp.MustCompile(`(?:"system"|system)\s*[:=]\s*([A-Za-z_][A-Za-z0-9_]*)`)
	reSystemBlock  = regexp.MustCompile(`(?:"system"|system)\s*[:=]\s*\[`)
	reTextField    = regexp.MustCompile("(?:\"text\"|text)\\s*[:=]\\s*([A-Za-z_][A-Za-z0-9_]*|`)")
	reRoleSystem   = regexp.MustCompile("(?s)(?:\"role\"|role)\\s*[:=]?\\s*[\"']system[\"']\\s*,\\s*(?:\"content\"|content)\\s*[:=]\\s*([A-Za-z_][A-Za-z0-9_]*|[\"'`])")
)

func systemPromptChars(content string, winStart, winEnd int) int {
	window := content[winStart:winEnd]
	if loc := reSystemInline.FindStringSubmatchIndex(window); loc != nil {
		delim := window[loc[2]:loc[3]]
		if n, ok := literalLen(content, winStart+loc[3], delim); ok {
			return n
		}
	}
	if m := reSystemIdent.FindStringSubmatch(window); m != nil {
		if n, ok := resolveConst(content, m[1]); ok {
			return n
		}
	}
	if loc := reSystemBlock.FindStringIndex(window); loc != nil {
		tail := window[loc[1]:min(loc[1]+300, len(window))]
		if m := reTextField.FindStringSubmatchIndex(tail); m != nil {
			value := tail[m[2]:m[3]]
			if value == "`" {
				if n, ok := literalLen(content, winStart+loc[1]+m[3], "`"); ok {
					return n
				}
			} else if n, ok := resolveConst(content, value); ok {
				return n
			}
		}
	}
	if m := reRoleSystem.FindStringSubmatchIndex(window); m != nil {
		value := window[m[2]:m[3]]
		switch value {
		case `"`, "'", "`":
			if n, ok := literalLen(content, winStart+m[3], value); ok {
				return n
			}
		default:
			if n, ok := resolveConst(content, value); ok {
				return n
			}
		}
	}
	return 0
}

// literalLen measures the string literal that starts right after start,
// whose opening delimiter was delim. Quotes stay on one line; backticks
// and triple quotes may span lines.
func literalLen(content string, start int, delim string) (int, bool) {
	rest := content[start:]
	if delim == `"` && strings.HasPrefix(rest, `""`) {
		// A Python triple quoted string: the first quote was consumed as
		// the delimiter, two more follow.
		end := strings.Index(rest[2:], `"""`)
		if end < 0 {
			return 0, false
		}
		return end, true
	}
	if delim == `"` || delim == "'" {
		limit := rest
		if nl := strings.IndexByte(rest, '\n'); nl >= 0 {
			limit = rest[:nl]
		}
		end := strings.Index(limit, delim)
		if end < 0 {
			return 0, false
		}
		return end, true
	}
	end := strings.Index(rest, delim)
	if end < 0 {
		return 0, false
	}
	return end, true
}

// resolveConst finds a same file assignment of name to a string literal
// and measures it. Backticks and Python triple quotes span lines.
func resolveConst(content, name string) (int, bool) {
	re := regexp.MustCompile(regexp.QuoteMeta(name) + "\\s*=\\s*(`|\"\"\"|\")")
	m := re.FindStringSubmatchIndex(content)
	if m == nil {
		return 0, false
	}
	delim := content[m[2]:m[3]]
	rest := content[m[3]:]
	switch delim {
	case "`":
		if end := strings.Index(rest, "`"); end >= 0 {
			return end, true
		}
	case `"""`:
		if end := strings.Index(rest, `"""`); end >= 0 {
			return end, true
		}
	case `"`:
		limit := rest
		if nl := strings.IndexByte(rest, '\n'); nl >= 0 {
			limit = rest[:nl]
		}
		if end := strings.Index(limit, `"`); end >= 0 {
			return end, true
		}
	}
	return 0, false
}
