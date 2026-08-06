package scan

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"path"
	"regexp"
	"strconv"
	"strings"
)

// windowLines bounds the fallback window used when no call extent can
// be found (config files, env files, languages the extent walker does
// not understand). Extent scoped extraction is the primary path.
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
	reCallish     = regexp.MustCompile(`\.create\(|generateText|generateObject|streamText|completions|embeddings|\.stream\(|messages\.create|\.builder\(`)
	reZodField    = regexp.MustCompile(`:\s*z\.`)
	reSchemaRef   = regexp.MustCompile(`(?:schema|tools)\s*[:=]\s*\[?\s*([A-Za-z_][A-Za-z0-9_]*)`)
	reEffort      = regexp.MustCompile(`(?i)["']?(?:reasoning_)?effort["']?\s*[:=]\s*["']?(minimal|low|medium|high|xhigh|max)`)
	reRetries     = regexp.MustCompile(`(?i)max_?retries["']?\s*[:=]\s*([0-9]+)`)
	reDimensions  = regexp.MustCompile(`(?i)["']?dimensions["']?\s*[:=]\s*([0-9]+)`)
	reDetailHigh  = regexp.MustCompile(`(?i)["']?detail["']?\s*[:=]\s*["']high["']`)
)

// analyzer carries the walked files so shape extraction and prompt
// resolution can follow one import hop inside the scanned repo, and
// caches the masked view of each file.
type analyzer struct {
	byPath map[string]string
	masks  map[string]masked
	spans  map[string][]span
}

func newAnalyzer(files []file) *analyzer {
	a := &analyzer{
		byPath: make(map[string]string, len(files)),
		masks:  map[string]masked{},
		spans:  map[string][]span{},
	}
	for _, f := range files {
		a.byPath[f.path] = string(f.data)
	}
	return a
}

func (a *analyzer) masked(p string) masked {
	m, ok := a.masks[p]
	if !ok {
		m = maskFile(p, a.byPath[p])
		a.masks[p] = m
	}
	return m
}

// siteHash fingerprints the call site's own text so the baseline
// ratchet survives line drift. Extent sites hash the prose masked
// extent with whitespace collapsed: moving the call or editing prompt
// prose changes nothing, changing the call's parameters does. Fallback
// sites hash their own line only.
func (a *analyzer) siteHash(p string, line, regionStart, regionEnd int, hasExtent bool) string {
	var text string
	if hasExtent {
		text = a.masked(p).prose[regionStart:regionEnd]
	} else {
		content := a.byPath[p]
		starts := lineStarts(content)
		if line-1 < len(starts) {
			end := len(content)
			if line < len(starts) {
				end = starts[line]
			}
			text = content[starts[line-1]:end]
		}
	}
	sum := sha256.Sum256([]byte(strings.Join(strings.Fields(text), " ")))
	return hex.EncodeToString(sum[:])[:16]
}

// hitOffset converts a one based line and column to a byte offset.
func hitOffset(content string, line, col int) int {
	starts := lineStarts(content)
	if line-1 >= len(starts) {
		return 0
	}
	return min(starts[line-1]+col, len(content))
}

// regionFor picks the byte range the shape and archetype layers read:
// the call extent when one exists, expanded to keep the call name in
// view, else the legacy line window. extStart is the extent's opening
// bracket, which the structural parser needs unexpanded. Builder
// languages bound the whole chained statement instead; wrapper calls
// with no model key fall back to the innermost balanced extent.
func (a *analyzer) regionFor(p string, line, col int) (regionStart, regionEnd, extStart int, hasExtent bool) {
	content := a.byPath[p]
	hit := hitOffset(content, line, col)
	m := a.masked(p)
	if builderFamily(p) {
		if s, e, ok := builderExtent(m.all, hit); ok {
			return headExpand(content, s), e, s, true
		}
	}
	if s, e, ok := callExtent(m.all, m.prose, hit); ok {
		return headExpand(content, s), e, s, true
	}
	if s, e, ok := innermostExtent(m.all, hit); ok {
		return headExpand(content, s), e, s, true
	}
	s, e := windowBounds(content, line)
	return s, e, 0, false
}

func (a *analyzer) extractShape(p string, regionStart, regionEnd, extStart int, hasExtent bool) Shape {
	m := a.masked(p)
	region := m.prose[regionStart:regionEnd]

	var s Shape
	s.BatchContext = reBatchCtx.MatchString(m.prose)
	s.BatchAPI = reBatchAPI.MatchString(m.prose)

	if match := reTemperature.FindStringSubmatch(region); match != nil {
		if v, err := strconv.ParseFloat(match[1], 64); err == nil {
			s.Temperature = &v
		}
	}
	if match := reMaxTokens.FindStringSubmatch(region); match != nil {
		if v, err := strconv.Atoi(strings.ReplaceAll(match[1], "_", "")); err == nil {
			s.MaxTokens = &v
		}
	}
	s.Tools = reTools.MatchString(region)
	s.ForcedTool = reForcedTool.MatchString(region)
	s.Streaming = reStreaming.MatchString(region)
	s.CacheControl = reCache.MatchString(region)
	s.EmbeddingCall = reEmbedding.MatchString(region)

	refText := a.resolveSchemaRef(p, region)
	s.JSONSchema = reSchema.MatchString(region) || reSchema.MatchString(refText)
	schemaText := refText
	if schemaText == "" {
		schemaText = a.byPath[p][regionStart:regionEnd]
	}
	s.SchemaEnumOnly, s.SchemaMultiField = schemaFacts(schemaText)

	if m := reEffort.FindStringSubmatch(region); m != nil {
		s.Effort = strings.ToLower(m[1])
	}
	if m := reRetries.FindStringSubmatch(region); m != nil {
		if v, err := strconv.Atoi(m[1]); err == nil {
			s.MaxRetries = &v
		}
	}
	if m := reDimensions.FindStringSubmatch(region); m != nil {
		if v, err := strconv.Atoi(m[1]); err == nil {
			s.Dimensions = &v
		}
	}
	s.ImageDetailHigh = reDetailHigh.MatchString(region)

	// For property and builder languages the structural parser has the
	// final word on the fields it decides.
	if hasExtent && propsFamily(p) {
		if info := parseCall(a.byPath[p], m, extStart, regionEnd); info != nil {
			applyCallInfo(&s, a.byPath[p], info)
		}
	}
	if hasExtent && builderFamily(p) {
		if info := builderParse(a.byPath[p], m, extStart, regionEnd); info != nil {
			applyCallInfo(&s, a.byPath[p], info)
		}
	}
	s.SystemPromptText = a.systemPromptText(p, regionStart, regionEnd)
	s.SystemPromptChars = len(s.SystemPromptText)
	s.Readable = reCallish.MatchString(region) ||
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

// windowBounds returns the byte range of the fallback detection window
// around a one based line number.
func windowBounds(content string, line int) (int, int) {
	starts := lineStarts(content)
	startLine := max(0, line-1-windowLines)
	endLine := min(len(starts), line+windowLines)
	winStart := starts[startLine]
	winEnd := len(content)
	if endLine < len(starts) {
		winEnd = starts[endLine]
	}
	return winStart, winEnd
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

// System prompt size and text, tried in order: an inline literal after
// system, an identifier after system resolved in the same file or one
// import hop away, a system content block list with a text field, and
// the chat completions style system role message. Anything else reports
// empty, which the rules treat as "not visible", never as "small".
var (
	reSystemInline = regexp.MustCompile("(?:\"system\"|system)\\s*[:=]\\s*([\"'`])")
	reSystemIdent  = regexp.MustCompile(`(?:"system"|system)\s*[:=]\s*([A-Za-z_][A-Za-z0-9_]*)`)
	reSystemBlock  = regexp.MustCompile(`(?:"system"|system)\s*[:=]\s*\[`)
	reTextField    = regexp.MustCompile("(?:\"text\"|text)\\s*[:=]\\s*([A-Za-z_][A-Za-z0-9_]*|`)")
	reRoleSystem   = regexp.MustCompile("(?s)(?:\"role\"|role)\\s*[:=]?\\s*[\"']system[\"']\\s*,\\s*(?:\"content\"|content)\\s*[:=]\\s*([A-Za-z_][A-Za-z0-9_$]*|[\"'`])")
)

func (a *analyzer) systemPromptText(p string, regionStart, regionEnd int) string {
	content := a.byPath[p]
	region := content[regionStart:regionEnd]
	if loc := reSystemInline.FindStringSubmatchIndex(region); loc != nil {
		delim := region[loc[2]:loc[3]]
		if text, ok := literalText(content, regionStart+loc[3], delim); ok {
			return text
		}
	}
	if m := reSystemIdent.FindStringSubmatch(region); m != nil {
		if text, ok := a.resolveConstText(p, m[1]); ok {
			return text
		}
	}
	if loc := reSystemBlock.FindStringIndex(region); loc != nil {
		tail := region[loc[1]:min(loc[1]+300, len(region))]
		if m := reTextField.FindStringSubmatchIndex(tail); m != nil {
			value := tail[m[2]:m[3]]
			if value == "`" {
				if text, ok := literalText(content, regionStart+loc[1]+m[3], "`"); ok {
					return text
				}
			} else if text, ok := a.resolveConstText(p, value); ok {
				return text
			}
		}
	}
	if m := reRoleSystem.FindStringSubmatchIndex(region); m != nil {
		value := region[m[2]:m[3]]
		switch value {
		case `"`, "'", "`":
			if text, ok := literalText(content, regionStart+m[3], value); ok {
				return text
			}
		default:
			if text, ok := a.resolveConstText(p, value); ok {
				return text
			}
		}
	}
	return ""
}

// literalText reads the string literal that starts right after start,
// whose opening delimiter was delim. Quotes stay on one line; backticks
// and triple quotes may span lines.
func literalText(content string, start int, delim string) (string, bool) {
	rest := content[start:]
	if delim == `"` && strings.HasPrefix(rest, `""`) {
		end := strings.Index(rest[2:], `"""`)
		if end < 0 {
			return "", false
		}
		return rest[2 : 2+end], true
	}
	if delim == `"` || delim == "'" {
		limit := rest
		if nl := strings.IndexByte(rest, '\n'); nl >= 0 {
			limit = rest[:nl]
		}
		end := strings.Index(limit, delim)
		if end < 0 {
			return "", false
		}
		return limit[:end], true
	}
	end := strings.Index(rest, delim)
	if end < 0 {
		return "", false
	}
	return rest[:end], true
}

var (
	reImportJS   = regexp.MustCompile(`import\s*\{([^}]+)\}\s*from\s*["'](\.{1,2}/[\w./-]+)["']`)
	reExportFrom = regexp.MustCompile(`export\s*\{([^}]+)\}\s*from\s*["'](\.{1,2}/[\w./-]+)["']`)
	reImportBare = regexp.MustCompile(`import\s*\{([^}]+)\}\s*from\s*["']([@\w][\w./-]*)["']`)
	reRequireJS  = regexp.MustCompile(`(?:const|let|var)\s*\{([^}]+)\}\s*=\s*require\(\s*["'](\.{1,2}/[\w./-]+)["']\s*\)`)
	rePyImport   = regexp.MustCompile(`from\s+([\w.]+)\s+import\s+([\w, ]+)`)
)

// resolveConstText finds a string constant by name: first in the same
// file, then across import hops inside the scanned repo, up to three
// hops with a cycle guard. Never outside the repo.
func (a *analyzer) resolveConstText(p, name string) (string, bool) {
	return a.resolveConstHop(p, name, 0, map[string]bool{})
}

func (a *analyzer) resolveConstHop(p, name string, depth int, seen map[string]bool) (string, bool) {
	if depth > 3 || seen[p+"\x00"+name] {
		return "", false
	}
	seen[p+"\x00"+name] = true
	content := a.byPath[p]
	if text, ok := resolveConstIn(content, name); ok {
		return text, true
	}
	for _, target := range a.importTargets(p, name) {
		if _, ok := a.byPath[target]; ok {
			if text, ok := a.resolveConstHop(target, name, depth+1, seen); ok {
				return text, true
			}
		}
	}
	return "", false
}

func resolveConstIn(content, name string) (string, bool) {
	re := regexp.MustCompile(regexp.QuoteMeta(name) + "\\s*=\\s*(`|\"\"\"|\")")
	m := re.FindStringSubmatchIndex(content)
	if m == nil {
		return "", false
	}
	return literalText(content, m[3], content[m[2]:m[3]])
}

// importTargets lists candidate repo paths that might define name,
// based on the file's own import statements, tsconfig path aliases,
// and a suffix search as the last resort for workspace layouts.
func (a *analyzer) importTargets(p, name string) []string {
	content := a.byPath[p]
	dir := path.Dir(p)
	var targets []string
	jsExts := []string{".ts", ".tsx", ".js", ".jsx", ".mjs", ".cjs"}
	add := func(spec string, exts []string) {
		base := path.Clean(path.Join(dir, spec))
		for _, ext := range exts {
			targets = append(targets, base+ext)
		}
	}
	addAliased := func(spec string) {
		for _, resolved := range a.tsconfigResolve(spec) {
			for _, ext := range jsExts {
				targets = append(targets, path.Clean(resolved)+ext)
			}
		}
		// Workspace fallback: any repo file whose path ends with the
		// spec, tried with each extension.
		for _, ext := range jsExts {
			suffix := spec + ext
			for known := range a.byPath {
				if strings.HasSuffix(known, suffix) {
					targets = append(targets, known)
				}
			}
		}
	}
	for _, re := range []*regexp.Regexp{reImportJS, reExportFrom, reRequireJS, reImportBare} {
		for _, m := range re.FindAllStringSubmatch(content, -1) {
			if !importsName(m[1], name) {
				continue
			}
			if strings.HasPrefix(m[2], ".") {
				add(m[2], jsExts)
			} else {
				addAliased(m[2])
			}
		}
	}
	for _, m := range rePyImport.FindAllStringSubmatch(content, -1) {
		if importsName(m[2], name) {
			module := strings.TrimLeft(m[1], ".")
			spec := strings.ReplaceAll(module, ".", "/")
			add(spec, []string{".py"})
			targets = append(targets, path.Clean(spec)+".py")
		}
	}
	return targets
}

// tsconfigResolve expands a non relative import spec through the
// compilerOptions paths of any tsconfig.json in the repo.
func (a *analyzer) tsconfigResolve(spec string) []string {
	var out []string
	for known, content := range a.byPath {
		if path.Base(known) != "tsconfig.json" && path.Base(known) != "jsconfig.json" {
			continue
		}
		var cfg struct {
			CompilerOptions struct {
				BaseURL string              `json:"baseUrl"`
				Paths   map[string][]string `json:"paths"`
			} `json:"compilerOptions"`
		}
		if err := json.Unmarshal([]byte(content), &cfg); err != nil {
			continue
		}
		root := path.Dir(known)
		base := path.Join(root, cfg.CompilerOptions.BaseURL)
		for pattern, subs := range cfg.CompilerOptions.Paths {
			prefix := strings.TrimSuffix(pattern, "*")
			if !strings.HasPrefix(spec, prefix) {
				continue
			}
			rest := strings.TrimPrefix(spec, prefix)
			for _, sub := range subs {
				out = append(out, path.Join(base, strings.TrimSuffix(sub, "*")+rest))
			}
		}
	}
	return out
}

func importsName(list, name string) bool {
	for _, part := range strings.Split(list, ",") {
		if strings.TrimSpace(part) == name {
			return true
		}
	}
	return false
}
