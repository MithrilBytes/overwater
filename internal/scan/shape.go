package scan

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"unicode/utf8"
)

// windowLines bounds the fallback window used when no call extent can
// be found (config files, env files, languages the extent walker does
// not understand). Extent scoped extraction is the primary path.
const windowLines = 30

// windowMaxBytes bounds that same window in bytes on each side of the
// hit. Thirty lines of real config or source stays well under it, so
// ordinary files keep the window they had.
const windowMaxBytes = 2000

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

// analyzer carries the walked files so shape extraction and prompt
// resolution can follow one import hop inside the scanned repo, and
// caches the masked view of each file.
type analyzer struct {
	byPath map[string]string
	paths  []string // sorted byPath keys; candidate walks stay deterministic
	// mu guards the lazy caches below; byPath is complete before any
	// worker starts and is then read only.
	mu    sync.Mutex
	masks map[string]masked
	spans map[string][]span
	lines map[string][]int
	facts map[string]fileFacts
	// factsRuns counts fileFacts evaluations so a test can prove the file
	// scoped regexes run per file, not per site. An atomic add on a path
	// that already runs two regexes costs nothing in production.
	factsRuns atomic.Int64
}

func newAnalyzer(files []file) *analyzer {
	a := &analyzer{
		byPath: make(map[string]string, len(files)),
		masks:  map[string]masked{},
		spans:  map[string][]span{},
		lines:  map[string][]int{},
		facts:  map[string]fileFacts{},
	}
	for _, f := range files {
		a.byPath[f.path] = string(f.data)
	}
	a.paths = make([]string, 0, len(a.byPath))
	for p := range a.byPath {
		a.paths = append(a.paths, p)
	}
	sort.Strings(a.paths)
	return a
}

func (a *analyzer) masked(p string) masked {
	a.mu.Lock()
	m, ok := a.masks[p]
	a.mu.Unlock()
	if ok {
		return m
	}
	// Computed outside the lock; a rare duplicate computation when two
	// workers cross into the same file beats serializing all masking.
	m = maskFile(p, a.byPath[p])
	a.mu.Lock()
	a.masks[p] = m
	a.mu.Unlock()
	return m
}

// factsFor returns the file scoped shape facts, computing them at most
// once per file in the common case.
func (a *analyzer) factsFor(p string) fileFacts {
	a.mu.Lock()
	f, ok := a.facts[p]
	a.mu.Unlock()
	if ok {
		return f
	}
	prose := a.masked(p).prose
	f = fileFacts{
		batchContext: reBatchCtx.MatchString(prose),
		batchAPI:     reBatchAPI.MatchString(prose),
	}
	a.factsRuns.Add(1)
	a.mu.Lock()
	a.facts[p] = f
	a.mu.Unlock()
	return f
}

// lineStartsFor caches the line index of a file. Rebuilding it per call
// site is a full pass over the file each time, which is the same
// quadratic trap as the file wide regexes.
func (a *analyzer) lineStartsFor(p string) []int {
	a.mu.Lock()
	s, ok := a.lines[p]
	a.mu.Unlock()
	if ok {
		return s
	}
	s = lineStarts(a.byPath[p])
	a.mu.Lock()
	a.lines[p] = s
	a.mu.Unlock()
	return s
}

// siteHash fingerprints the call site's own text so the baseline
// ratchet survives line drift. Extent sites hash the prose masked
// extent with whitespace collapsed: moving the call or editing prompt
// prose changes nothing, changing the call's parameters does. Fallback
// sites hash their own line only.
func (a *analyzer) siteHash(p string, line int, r region) string {
	var text string
	if r.isExtent {
		text = a.masked(p).prose[r.start:r.end]
	} else {
		content := a.byPath[p]
		starts := a.lineStartsFor(p)
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

// hitOffsetIn converts a one based line and column to a byte offset.
func (a *analyzer) hitOffsetIn(p string, line, col int) int {
	starts := a.lineStartsFor(p)
	if line-1 >= len(starts) {
		return 0
	}
	return min(starts[line-1]+col, len(a.byPath[p]))
}

// region is the slice of a file that layers 3 and 4 read for one model
// reference.
type region struct {
	start, end  int  // byte range, expanded to keep the call name in view
	extentStart int  // the extent's opening bracket, which the structural parsers need unexpanded
	isExtent    bool // false when no extent was found and start:end is the fallback window
	hit         int  // byte offset of the model reference itself
}

// regionFor bounds the call around a model reference: the call extent
// when one exists, else the fallback line window. Builder languages
// bound the whole chained statement instead; wrapper calls with no
// model key fall back to the innermost balanced extent.
func (a *analyzer) regionFor(p string, line, col int) region {
	content := a.byPath[p]
	hit := a.hitOffsetIn(p, line, col)
	m := a.masked(p)
	extent := func(s, e int) region {
		return region{start: headExpand(content, s), end: e, extentStart: s, isExtent: true, hit: hit}
	}
	if builderFamily(p) {
		if s, e, ok := builderExtent(m.all, hit); ok {
			return extent(s, e)
		}
	}
	if s, e, ok := callExtent(m.all, m.prose, hit); ok {
		return extent(s, e)
	}
	if s, e, ok := innermostExtent(m.all, hit); ok {
		return extent(s, e)
	}
	s, e := windowBounds(content, a.lineStartsFor(p), line, hit)
	return region{start: s, end: e, hit: hit}
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

// windowBounds returns the byte range of the fallback detection window
// around the hit at a one based line number. Lines are the wrong unit in
// a minified file, which is one line however large, so the window is
// also bounded in bytes around the hit; without that bound every
// reference in such a file reads the whole file and scan cost is
// quadratic in references per file.
func windowBounds(content string, starts []int, line, hit int) (int, int) {
	startLine := max(0, line-1-windowLines)
	endLine := min(len(starts), line+windowLines)
	winStart := starts[startLine]
	winEnd := len(content)
	if endLine < len(starts) {
		winEnd = starts[endLine]
	}
	return max(winStart, hit-windowMaxBytes), min(winEnd, hit+windowMaxBytes)
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
	// A block's text may be an identifier or a literal in any of the
	// spellings literalText reads, inline string literals included: an
	// inline prompt is the same prompt as a named one and must measure the
	// same, not zero.
	reTextField  = regexp.MustCompile("(?:\"text\"|text)\\s*[:=]\\s*([A-Za-z_][A-Za-z0-9_]*|[\"'`])")
	reRoleSystem = regexp.MustCompile("(?s)(?:\"role\"|role)\\s*[:=]?\\s*[\"']system[\"']\\s*,\\s*(?:\"content\"|content)\\s*[:=]\\s*([A-Za-z_][A-Za-z0-9_$]*|[\"'`])")
)

func (a *analyzer) systemPromptText(p string, r region) string {
	content := a.byPath[p]
	text := content[r.start:r.end]
	if loc := reSystemInline.FindStringSubmatchIndex(text); loc != nil {
		delim := text[loc[2]:loc[3]]
		if lit, ok := literalText(content, r.start+loc[3], delim); ok {
			return lit
		}
	}
	if m := reSystemIdent.FindStringSubmatch(text); m != nil {
		if lit, ok := a.resolveConstText(p, m[1]); ok {
			return lit
		}
	}
	if loc := reSystemBlock.FindStringIndex(text); loc != nil {
		tail := text[loc[1]:min(loc[1]+300, len(text))]
		if m := reTextField.FindStringSubmatchIndex(tail); m != nil {
			if lit, ok := a.promptValue(p, content, tail[m[2]:m[3]], r.start+loc[1]+m[3]); ok {
				return lit
			}
		}
	}
	if m := reRoleSystem.FindStringSubmatchIndex(text); m != nil {
		if lit, ok := a.promptValue(p, content, text[m[2]:m[3]], r.start+m[3]); ok {
			return lit
		}
	}
	return ""
}

// promptValue reads a prompt that the regex captured either as an
// opening delimiter, in which case the literal starts at from, or as an
// identifier to resolve.
func (a *analyzer) promptValue(p, content, value string, from int) (string, bool) {
	switch value {
	case `"`, "'", "`":
		return literalText(content, from, value)
	default:
		return a.resolveConstText(p, value)
	}
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
	if delim == "'" && strings.HasPrefix(rest, "''") {
		end := strings.Index(rest[2:], "'''")
		if end < 0 {
			return "", false
		}
		return rest[2 : 2+end], true
	}
	if delim == `"` || delim == "'" {
		// Escape aware, mirroring the masker: an escaped quote is part
		// of the string, and an unescaped newline ends the search.
		q := delim[0]
		for i := 0; i < len(rest); i++ {
			switch rest[i] {
			case '\\':
				i++
			case '\n':
				return "", false
			case q:
				return rest[:i], true
			}
		}
		return "", false
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
	// The name needs a left boundary: resolving PROMPT must not match
	// the tail of LEGACY_PROMPT. Triple quotes come before their single
	// char forms so the longer delimiter wins.
	re := regexp.MustCompile(`(?m)(?:^|[^A-Za-z0-9_$])` + regexp.QuoteMeta(name) + "\\s*=\\s*(`|\"\"\"|'''|\"|')")
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
		// spec, tried with each extension, in sorted path order so
		// ambiguous candidates resolve the same way on every run.
		for _, ext := range jsExts {
			suffix := spec + ext
			for _, known := range a.paths {
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
// compilerOptions paths of any tsconfig.json in the repo. Configs and
// their path patterns are visited in sorted order, keeping ambiguous
// alias resolution stable across runs.
func (a *analyzer) tsconfigResolve(spec string) []string {
	var out []string
	for _, known := range a.paths {
		if path.Base(known) != "tsconfig.json" && path.Base(known) != "jsconfig.json" {
			continue
		}
		var cfg struct {
			CompilerOptions struct {
				BaseURL string              `json:"baseUrl"`
				Paths   map[string][]string `json:"paths"`
			} `json:"compilerOptions"`
		}
		if err := json.Unmarshal([]byte(jsonStripComments(a.byPath[known])), &cfg); err != nil {
			continue
		}
		root := path.Dir(known)
		base := path.Join(root, cfg.CompilerOptions.BaseURL)
		patterns := make([]string, 0, len(cfg.CompilerOptions.Paths))
		for pattern := range cfg.CompilerOptions.Paths {
			patterns = append(patterns, pattern)
		}
		sort.Strings(patterns)
		for _, pattern := range patterns {
			prefix := strings.TrimSuffix(pattern, "*")
			if !strings.HasPrefix(spec, prefix) {
				continue
			}
			rest := strings.TrimPrefix(spec, prefix)
			for _, sub := range cfg.CompilerOptions.Paths[pattern] {
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
