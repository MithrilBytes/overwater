package scan

import (
	"path"
	"regexp"
	"strconv"
	"strings"
)

// Structural parsing for JS and TS call sites: instead of regexing a
// window, read the call's callee chain and the top level properties of
// the object literal that names the model. Pure Go on purpose; the
// tree-sitter bindings need cgo, which would break the static release
// binaries the action pins by checksum.

// callInfo is one parsed call expression.
type callInfo struct {
	Callee string
	Props  map[string]string // raw value text by property name
}

func jsFamily(p string) bool {
	switch path.Ext(p) {
	case ".js", ".jsx", ".ts", ".tsx", ".mjs", ".cjs":
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

// parseCall reads the call whose object literal argument spans
// [objStart, objEnd) in the original content. Structure comes from the
// fully masked view so braces in strings cannot mislead it; values come
// from the original so prompt literals stay intact.
func parseCall(content string, m masked, objStart, objEnd int) *callInfo {
	if objStart >= len(content) || content[objStart] != '{' {
		return nil
	}
	info := &callInfo{Props: parseProps(content, m.all, m.prose, objStart+1, objEnd-1)}
	info.Callee = calleeChain(m.all, objStart)
	return info
}

// calleeChain walks left from the object literal to the identifier
// chain calling it, crossing whitespace only across a dot so
// "await client" never glues into one name.
func calleeChain(masked string, objStart int) string {
	i := objStart - 1
	for i >= 0 && isWS(masked[i]) {
		i--
	}
	if i < 0 || masked[i] != '(' {
		return ""
	}
	i--
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

// parseProps extracts the top level key: value pairs of an object
// literal region. Depth counting uses the fully masked text, key names
// the prose view (so quoted keys keep their text), values the original.
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
		switch maskedAll[i] {
		case '{', '[', '(':
			depth++
		case '}', ']', ')':
			depth--
		case ':':
			if depth == 0 && valueStart < 0 {
				key = keyBefore(prose, start, i)
				valueStart = i + 1
			}
		case ',':
			if depth == 0 {
				flush(i)
			}
		}
	}
	flush(end)
	return props
}

// keyBefore reads the property name that ends just before the colon at
// position colon, unquoting it when quoted. Computed keys return empty.
func keyBefore(prose string, limit, colon int) string {
	i := colon - 1
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

var reLeadingNumber = regexp.MustCompile(`^[0-9]*\.?[0-9]+`)

// propNumber finds a numeric property by any of the given names,
// looking one level into config style wrapper objects too, which is
// where the Gemini SDK keeps temperature.
func propNumber(content string, info *callInfo, names ...string) (float64, bool) {
	for _, n := range names {
		if v, ok := info.Props[n]; ok {
			if m := reLeadingNumber.FindString(strings.TrimSpace(v)); m != "" {
				f, err := strconv.ParseFloat(m, 64)
				if err == nil {
					return f, true
				}
			}
		}
	}
	for _, wrapper := range []string{"config", "generationConfig", "generation_config"} {
		v, ok := info.Props[wrapper]
		if !ok || !strings.HasPrefix(strings.TrimSpace(v), "{") {
			continue
		}
		inner := propsFromObjectText(strings.TrimSpace(v))
		for _, n := range names {
			if raw, ok := inner[n]; ok {
				if m := reLeadingNumber.FindString(strings.TrimSpace(raw)); m != "" {
					if f, err := strconv.ParseFloat(m, 64); err == nil {
						return f, true
					}
				}
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
		"max_tokens", "maxTokens", "max_output_tokens", "maxOutputTokens",
		"max_completion_tokens", "maxCompletionTokens"); ok {
		iv := int(v)
		s.MaxTokens = &iv
	} else {
		s.MaxTokens = nil
	}
	_, s.Tools = info.Props["tools"]
	choice := info.Props["tool_choice"] + info.Props["toolChoice"]
	s.ForcedTool = strings.Contains(choice, `"tool"`) || strings.Contains(choice, "'tool'")
	stream := strings.TrimSpace(info.Props["stream"])
	s.Streaming = stream == "true" ||
		strings.HasSuffix(info.Callee, ".stream") ||
		strings.Contains(info.Callee, "streamText")
	for _, k := range []string{"schema", "response_format", "responseFormat", "responseSchema", "input_schema", "output_config"} {
		if _, ok := info.Props[k]; ok {
			s.JSONSchema = true
		}
	}
	if strings.Contains(strings.ToLower(info.Callee), "embed") {
		s.EmbeddingCall = true
	}
}
