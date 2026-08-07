package scan

import (
	"path"
	"regexp"
	"strconv"
	"strings"
)

// Structural parsing for call sites: instead of regexing a window, read
// the call's callee chain and the properties of the argument that names
// the model. Pure Go on purpose; tree-sitter bindings need cgo, which
// would break the static release binaries the action pins by checksum.
//
// Two shapes cover the supported languages: property style (key: value
// or key = value inside an object literal or argument list) and builder
// style (.model("x").maxTokens(1024) chains).

// callInfo is one parsed call expression.
type callInfo struct {
	Callee string
	Props  map[string]string // raw value text by property name, as written
}

func jsFamily(p string) bool {
	switch path.Ext(p) {
	case ".js", ".jsx", ".ts", ".tsx", ".mjs", ".cjs":
		return true
	}
	return false
}

// propsFamily lists the languages whose calls read as key value pairs.
func propsFamily(p string) bool {
	if jsFamily(p) {
		return true
	}
	switch path.Ext(p) {
	case ".py", ".ipynb", ".rb", ".go", ".cs", ".php", ".sh", ".bash", ".zsh", ".json":
		return true
	}
	return false
}

// builderFamily lists the languages whose calls read as method chains.
func builderFamily(p string) bool {
	switch path.Ext(p) {
	case ".java", ".kt", ".kts", ".rs", ".scala", ".swift":
		return true
	}
	return false
}

func isChainChar(b byte) bool {
	return b == '.' || b == '_' || b == '$' ||
		(b >= '0' && b <= '9') || (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}

func isWS(b byte) bool {
	return b == ' ' || b == '\t' || b == '\n' || b == '\r'
}

// parseCall reads the call whose argument extent spans [objStart,
// objEnd) in the original content. The extent may be an object literal
// or the call's own parenthesized arguments. Structure comes from the
// fully masked view so braces in strings cannot mislead it; values come
// from the original so prompt literals stay intact.
func parseCall(content string, m masked, objStart, objEnd int) *callInfo {
	if objStart >= len(content) {
		return nil
	}
	open := content[objStart]
	if open != '{' && open != '(' {
		return nil
	}
	info := &callInfo{Props: parseProps(content, m.all, m.prose, objStart+1, objEnd-1)}
	info.Callee = calleeChain(m.all, objStart)
	return info
}

// calleeChain walks left from the extent to the identifier chain
// calling it, crossing whitespace only across a dot so "await client"
// never glues into one name.
func calleeChain(masked string, objStart int) string {
	if masked[objStart] == '(' {
		return chainBefore(masked, objStart-1)
	}
	i := objStart - 1
	for i >= 0 && isWS(masked[i]) {
		i--
	}
	if i < 0 || masked[i] != '(' {
		return ""
	}
	return chainBefore(masked, i-1)
}

func chainBefore(masked string, from int) string {
	i := from
	var rev []byte
	for i >= 0 {
		c := masked[i]
		if isChainChar(c) {
			rev = append(rev, c)
			i--
			continue
		}
		if isWS(c) {
			j := i
			for j >= 0 && isWS(masked[j]) {
				j--
			}
			if j >= 0 && isChainChar(masked[j]) &&
				(masked[j] == '.' || (len(rev) > 0 && rev[len(rev)-1] == '.')) {
				i = j
				continue
			}
		}
		break
	}
	for l, r := 0, len(rev)-1; l < r; l, r = l+1, r-1 {
		rev[l], rev[r] = rev[r], rev[l]
	}
	return string(rev)
}

// parseProps extracts the top level key value pairs of an argument
// region. Separators are ':' and '=', guarded against comparisons,
// arrows, and walrus assignments. Depth counting uses the fully masked
// text, key names the prose view, values the original.
func parseProps(content, maskedAll, prose string, start, end int) map[string]string {
	props := map[string]string{}
	if start < 0 || end > len(maskedAll) || start >= end {
		return props
	}
	depth := 0
	valueStart := -1
	var key string
	flush := func(valueEnd int) {
		if key != "" && valueStart >= 0 {
			props[key] = strings.TrimSpace(content[valueStart:valueEnd])
		}
		key, valueStart = "", -1
	}
	for i := start; i < end; i++ {
		c := maskedAll[i]
		switch c {
		case '{', '[', '(':
			depth++
		case '}', ']', ')':
			depth--
		case ':', '=':
			if depth != 0 || valueStart >= 0 {
				continue
			}
			if i+1 < end {
				next := maskedAll[i+1]
				if next == '=' || next == '>' || next == ':' {
					continue
				}
			}
			if i > start {
				prev := maskedAll[i-1]
				if prev == '=' || prev == '<' || prev == '>' || prev == '!' || prev == ':' {
					continue
				}
			}
			key = keyBefore(prose, start, i)
			valueStart = i + 1
		case ',':
			if depth == 0 {
				flush(i)
			}
		}
	}
	flush(end)
	return props
}

// keyBefore reads the property name that ends just before the separator
// at position sep, unquoting it when quoted. Computed keys return empty.
func keyBefore(prose string, limit, sep int) string {
	i := sep - 1
	for i >= limit && isWS(prose[i]) {
		i--
	}
	end := i
	for i >= limit {
		c := prose[i]
		if isChainChar(c) || c == '"' || c == '\'' {
			i--
			continue
		}
		break
	}
	name := strings.Trim(prose[i+1:end+1], `"'`)
	if strings.ContainsAny(name, "[]") {
		return ""
	}
	return name
}

// normKey folds case and underscores so max_tokens, maxTokens, and
// MaxTokens all land on one name.
func normKey(k string) string {
	return strings.ReplaceAll(strings.ToLower(k), "_", "")
}

// propVal resolves a property by any of the given names, first name in
// the list winning. Within one name an exactly spelled key wins;
// otherwise the lexicographically smallest key folding to the same
// normalized name does, so duplicate spellings (max_tokens plus
// maxTokens) resolve identically on every run.
func propVal(info *callInfo, names ...string) (string, bool) {
	for _, n := range names {
		if v, ok := info.Props[n]; ok {
			return v, true
		}
		want := normKey(n)
		best, found := "", false
		for k := range info.Props {
			if normKey(k) != want {
				continue
			}
			if !found || k < best {
				best, found = k, true
			}
		}
		if found {
			return info.Props[best], true
		}
	}
	return "", false
}

var (
	reLeadingNumber = regexp.MustCompile(`^[0-9]*\.?[0-9]+`)
	reWrappedNumber = regexp.MustCompile(`^[A-Za-z_$.]+\(\s*([0-9]*\.?[0-9]+)\s*\)$`)
)

func numberFrom(raw string) (float64, bool) {
	raw = strings.TrimSpace(raw)
	if m := reLeadingNumber.FindString(raw); m != "" {
		if f, err := strconv.ParseFloat(m, 64); err == nil {
			return f, true
		}
	}
	// Wrapped constructors like anthropic.Int(64) still carry the number.
	if m := reWrappedNumber.FindStringSubmatch(raw); m != nil {
		if f, err := strconv.ParseFloat(m[1], 64); err == nil {
			return f, true
		}
	}
	return 0, false
}

// propNumber finds a numeric property by any of the given names,
// looking one level into config style wrapper objects too, which is
// where the Gemini SDK keeps temperature.
func propNumber(content string, info *callInfo, names ...string) (float64, bool) {
	if v, ok := propVal(info, names...); ok {
		if f, ok := numberFrom(v); ok {
			return f, true
		}
	}
	for _, wrapper := range []string{"config", "generationConfig", "generation_config"} {
		v, ok := propVal(info, wrapper)
		if !ok || !strings.HasPrefix(strings.TrimSpace(v), "{") {
			continue
		}
		inner := propsFromObjectText(strings.TrimSpace(v))
		wrapped := &callInfo{Props: inner}
		if raw, ok := propVal(wrapped, names...); ok {
			if f, ok := numberFrom(raw); ok {
				return f, true
			}
		}
	}
	return 0, false
}

// propsFromObjectText parses a standalone object literal string by
// masking it as if it were its own tiny source file.
func propsFromObjectText(text string) map[string]string {
	m := maskFile("w.ts", text)
	return parseProps(text, m.all, m.prose, 1, len(text)-1)
}

// applyCallInfo overrides the regex read shape with the parsed one for
// the fields the property list decides outright. Schema and embedding
// evidence only ever widen, since both can also come from resolved
// references the properties cannot see.
func applyCallInfo(s *Shape, content string, info *callInfo) {
	if v, ok := propNumber(content, info, "temperature"); ok {
		s.Temperature = &v
	} else {
		s.Temperature = nil
	}
	if v, ok := propNumber(content, info,
		"max_tokens", "max_output_tokens", "max_completion_tokens", "maxTokens"); ok {
		iv := int(v)
		s.MaxTokens = &iv
	} else {
		s.MaxTokens = nil
	}
	_, s.Tools = propVal(info, "tools")
	choice, _ := propVal(info, "tool_choice")
	s.ForcedTool = strings.Contains(choice, `"tool"`) || strings.Contains(choice, "'tool'")
	stream, _ := propVal(info, "stream")
	s.Streaming = strings.TrimSpace(stream) == "true" ||
		strings.HasSuffix(info.Callee, ".stream") ||
		strings.Contains(info.Callee, "streamText")
	for _, k := range []string{"schema", "response_format", "responseSchema", "input_schema", "output_config"} {
		if _, ok := propVal(info, k); ok {
			s.JSONSchema = true
		}
	}
	if raw, ok := propVal(info, "effort", "reasoning_effort"); ok {
		s.Effort = strings.Trim(strings.TrimSpace(raw), `"'`)
	}
	if v, ok := propNumber(content, info, "max_retries", "maxRetries", "retries"); ok {
		iv := int(v)
		s.MaxRetries = &iv
	}
	if v, ok := propNumber(content, info, "dimensions"); ok {
		iv := int(v)
		s.Dimensions = &iv
	}
	if detail, ok := propVal(info, "detail"); ok {
		d := strings.Trim(strings.TrimSpace(detail), `"'`)
		s.ImageDetailHigh = d == "high"
	}
	if strings.Contains(strings.ToLower(info.Callee), "embed") {
		s.EmbeddingCall = true
	}
}

// Builder chains: find the statement around the hit and read its
// .method(args) pairs as properties.
var reBuilderMethod = regexp.MustCompile(`\.([A-Za-z_][A-Za-z0-9_]*)\(`)

// builderExtent bounds the statement containing the hit: back to the
// previous ';', '{', or '}' and forward to the next ';' at depth zero.
func builderExtent(masked string, hit int) (int, int, bool) {
	start := hit
	for start > 0 {
		c := masked[start-1]
		if c == ';' || c == '{' || c == '}' {
			break
		}
		start--
		if hit-start > 2000 {
			return 0, 0, false
		}
	}
	depth := 0
	end := hit
	for end < len(masked) {
		switch masked[end] {
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			depth--
		case ';':
			if depth <= 0 {
				end++
				goto done
			}
		}
		end++
		if end-hit > 2000 {
			break
		}
	}
done:
	if !strings.Contains(strings.ToLower(masked[start:end]), ".model(") {
		return 0, 0, false
	}
	return start, end, true
}

// builderParse reads a builder statement into callInfo: each
// .method(args) pair becomes a property, and the leading chain the
// callee.
func builderParse(content string, m masked, start, end int) *callInfo {
	region := m.all[start:end]
	info := &callInfo{Props: map[string]string{}}
	for _, loc := range reBuilderMethod.FindAllStringSubmatchIndex(region, -1) {
		name := region[loc[2]:loc[3]]
		open := start + loc[1] - 1
		closer, ok := matchClose(m.all, open)
		if !ok || closer+1 > end {
			continue
		}
		info.Props[name] = strings.TrimSpace(content[open+1 : closer])
	}
	if idx := strings.Index(region, "."); idx > 0 {
		info.Callee = strings.TrimSpace(strings.Trim(region[:idx], "\n\t ="))
	}
	if _, ok := propVal(info, "stream"); ok || strings.Contains(strings.ToLower(region), "streaming") {
		info.Props["stream"] = "true"
	}
	return info
}
