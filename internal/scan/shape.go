package scan

import (
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

var (
	reTemperature = regexp.MustCompile(`(?i)["']?temperature["']?\s*[:=]\s*([0-9]*\.?[0-9]+)`)
	reMaxTokens   = regexp.MustCompile(`(?i)["']?max_?(?:output_?|completion_?)?tokens["']?\s*[:=]\s*([0-9][0-9_]*)`)
	reSchema      = regexp.MustCompile(`response_format|json_schema|input_schema|responseSchema|responseMimeType|generateObject`)
	reTools       = regexp.MustCompile(`(?i)["']?tools["']?\s*[:=]\s*\[`)
	reForcedTool  = regexp.MustCompile(`(?i)tool_?choice.{0,60}["']tool["']`)
	reStreaming   = regexp.MustCompile(`stream\s*[:=]\s*[Tt]rue|streamText\(|\.stream\(`)
	reCache       = regexp.MustCompile(`cache_control|cacheControl`)
	reEmbedding   = regexp.MustCompile(`embeddings\.create|embedContent|embed_content|\.embed\(`)
	reBatchAPI    = regexp.MustCompile(`batches\.create|/v1/batches|messages\.batches`)
	reBatchCtx    = regexp.MustCompile(`cron\.schedule|node-cron|crontab|schedule\.every|celery|BackgroundScheduler`)
	reCallish     = regexp.MustCompile(`\.create\(|generateText|generateObject|streamText|completions|embeddings|\.stream\(|messages\.create|\.builder\(`)
	reZodField    = regexp.MustCompile(`:\s*z\.`)
	reSchemaRef   = regexp.MustCompile(`(?:schema|tools)\s*[:=]\s*\[?\s*([A-Za-z_][A-Za-z0-9_]*)`)
	reEffort      = regexp.MustCompile(`(?i)["']?(?:reasoning_)?effort["']?\s*[:=]\s*["']?(minimal|low|medium|high|xhigh|max)\b`)
	reRetries     = regexp.MustCompile(`(?i)\b(?:max_?)?retries["']?\s*[:=]\s*([0-9]+)`)
	reDimensions  = regexp.MustCompile(`(?i)["']?dimensions["']?\s*[:=]\s*([0-9]+)`)
	reDetailHigh  = regexp.MustCompile(`(?i)["']?detail["']?\s*[:=]\s*["']high["']`)
)

// fileFacts are the shape facts a call site inherits from its whole
// file rather than from its own extent. Nothing in here reads the
// region, so it is computed once per file: evaluating it per site made
// scan cost quadratic in the number of model references, and a minified
// config full of model ids took minutes.
type fileFacts struct {
	batchContext bool
	batchAPI     bool
}

func readFileFacts(prose string) fileFacts {
	return fileFacts{
		batchContext: reBatchCtx.MatchString(prose),
		batchAPI:     reBatchAPI.MatchString(prose),
	}
}

func (a *analyzer) extractShape(p string, r region) Shape {
	m := a.masked(p)
	text := m.prose[r.start:r.end]

	var s Shape
	facts := a.factsFor(p)
	s.BatchContext = facts.batchContext
	s.BatchAPI = facts.batchAPI

	if match := reTemperature.FindStringSubmatch(text); match != nil {
		if v, err := strconv.ParseFloat(match[1], 64); err == nil {
			s.Temperature = &v
		}
	}
	if match := reMaxTokens.FindStringSubmatch(text); match != nil {
		if v, err := strconv.Atoi(strings.ReplaceAll(match[1], "_", "")); err == nil {
			s.MaxTokens = &v
		}
	}
	s.Tools = reTools.MatchString(text)
	s.ForcedTool = reForcedTool.MatchString(text)
	s.Streaming = reStreaming.MatchString(text)
	s.CacheControl = reCache.MatchString(text)
	s.EmbeddingCall = reEmbedding.MatchString(text)

	refText := a.resolveSchemaRef(p, text)
	s.JSONSchema = reSchema.MatchString(text) || reSchema.MatchString(refText)
	schemaText := refText
	if schemaText == "" {
		// Fall back to the prose masked text, never the raw one:
		// long string interiors are blank there, so a schema example
		// inside prompt prose cannot fake schema facts.
		schemaText = text
	}
	s.SchemaEnumOnly, s.SchemaMultiField = schemaFacts(schemaText)

	if m := reEffort.FindStringSubmatch(text); m != nil {
		s.Effort = strings.ToLower(m[1])
	}
	if m := reRetries.FindStringSubmatch(text); m != nil {
		if v, err := strconv.Atoi(m[1]); err == nil {
			s.MaxRetries = &v
		}
	}
	if m := reDimensions.FindStringSubmatch(text); m != nil {
		if v, err := strconv.Atoi(m[1]); err == nil {
			s.Dimensions = &v
		}
	}
	s.ImageDetailHigh = reDetailHigh.MatchString(text)

	// For property and builder languages the structural parser has the
	// final word on the fields it decides.
	if r.isExtent && propsFamily(p) {
		if info := parseCall(a.byPath[p], m, r.extentStart, r.end); info != nil {
			applyCallInfo(&s, info)
		}
	}
	if r.isExtent && builderFamily(p) {
		if info := builderParse(a.byPath[p], m, r.extentStart, r.end); info != nil {
			applyCallInfo(&s, info)
		}
	}
	s.SystemPromptText = a.systemPromptText(p, r)
	// Runes, not bytes: non ASCII prompts must not overcount against
	// token thresholds.
	s.SystemPromptChars = utf8.RuneCountInString(s.SystemPromptText)
	s.Readable = reCallish.MatchString(text) ||
		s.Temperature != nil || s.MaxTokens != nil || s.JSONSchema || s.Streaming
	return s
}

// resolveSchemaRef follows a schema: or tools: identifier to the
// constant it names in the same file and returns that constant's
// bracketed text, so a schema defined above the call still informs the
// shape.
func (a *analyzer) resolveSchemaRef(p, region string) string {
	m := reSchemaRef.FindStringSubmatch(region)
	if m == nil {
		return ""
	}
	return a.constExtent(p, m[1])
}

func (a *analyzer) constExtent(p, name string) string {
	content := a.byPath[p]
	m := a.masked(p)
	re := regexp.MustCompile(`(?m)^[ \t]*(?:const|let|var)?[ \t]*` + regexp.QuoteMeta(name) + `\s*=`)
	loc := re.FindStringIndex(m.prose)
	if loc == nil {
		return ""
	}
	tail := m.all[loc[1]:min(loc[1]+60, len(content))]
	rel := strings.IndexAny(tail, "([{")
	if rel < 0 {
		return ""
	}
	open := loc[1] + rel
	closer, ok := matchClose(m.all, open)
	if !ok {
		return ""
	}
	return content[open : closer+1]
}

// schemaFacts reads the semantics out of a schema: an enum only output
// is classification shaped, several free fields are extraction shaped.
func schemaFacts(text string) (enumOnly, multiField bool) {
	if text == "" {
		return false, false
	}
	if fields := len(reZodField.FindAllString(text, -1)); fields > 0 {
		enums := strings.Count(text, "z.enum(")
		return enums >= fields, fields >= 2 && enums < fields
	}
	props := propertiesExtent(text)
	if props == "" {
		return false, false
	}
	fields := strings.Count(props, `"type"`)
	enums := strings.Count(props, `"enum"`)
	if fields == 0 {
		return false, false
	}
	return enums >= fields, fields >= 2 && enums < fields
}

func propertiesExtent(text string) string {
	idx := strings.Index(text, `"properties"`)
	if idx < 0 {
		return ""
	}
	rel := strings.IndexByte(text[idx:], '{')
	if rel < 0 {
		return ""
	}
	open := idx + rel
	closer, ok := matchClose(text, open)
	if !ok {
		return ""
	}
	return text[open : closer+1]
}
