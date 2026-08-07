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

// Numbers may use underscore digit separators (32_768), which are
// stripped before parsing.
var (
	reLeadingNumber = regexp.MustCompile(`^(?:[0-9][0-9_]*(?:\.[0-9_]*)?|\.[0-9][0-9_]*)`)
	reWrappedNumber = regexp.MustCompile(`^[A-Za-z_$.]+\(\s*([0-9][0-9_]*(?:\.[0-9_]*)?|\.[0-9][0-9_]*)\s*\)$`)
)

func numberFrom(raw string) (float64, bool) {
	raw = strings.TrimSpace(raw)
	if m := reLeadingNumber.FindString(raw); m != "" {
		if f, err := strconv.ParseFloat(strings.ReplaceAll(m, "_", ""), 64); err == nil {
			return f, true
		}
	}
	// Wrapped constructors like anthropic.Int(64) still carry the number.
	if m := reWrappedNumber.FindStringSubmatch(raw); m != nil {
		if f, err := strconv.ParseFloat(strings.ReplaceAll(m[1], "_", ""), 64); err == nil {
			return f, true
		}
	}
	return 0, false
}

// propNumber finds a numeric property by any of the given names,
// looking one level into config style wrapper objects too, which is
// where the Gemini SDK keeps temperature.
func propNumber(info *callInfo, names ...string) (float64, bool) {
	if v, ok := propVal(info, names...); ok {
		if f, ok := numberFrom(v); ok {
			return f, true
		}
	}
	for _, wrapper := range []string{"config", "generationConfig", "generation_config"} {
		v, ok := propVal(info, wrapper)
		if !ok {
			continue
		}
		inner := wrapperProps(v)
		if inner == nil {
			continue
		}
		wrapped := &callInfo{Props: inner}
		if raw, ok := propVal(wrapped, names...); ok {
			if f, ok := numberFrom(raw); ok {
				return f, true
			}
		}
	}
	return 0, false
}

// applyCallInfo overrides the regex read shape with the parsed one for
// the fields the property list decides outright. Schema and embedding
// evidence only ever widen, since both can also come from resolved
// references the properties cannot see.
func applyCallInfo(s *Shape, info *callInfo) {
	if v, ok := propNumber(info, "temperature"); ok {
		s.Temperature = &v
	} else {
		s.Temperature = nil
	}
	if v, ok := propNumber(info,
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
	// Case insensitive: Python writes True.
	s.Streaming = strings.EqualFold(strings.TrimSpace(stream), "true") ||
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
	if v, ok := propNumber(info, "max_retries", "maxRetries", "retries"); ok {
		iv := int(v)
		s.MaxRetries = &iv
	}
	if v, ok := propNumber(info, "dimensions"); ok {
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
