package scan

import (
	"path"
	"regexp"
	"strconv"
	"strings"
)

// Structural parsing reads a call's callee chain and the properties of
// the argument that names the model, rather than regexing a window. No
// cgo: the release binaries are static and the action pins them by
// checksum, which tree-sitter bindings would break.
//
// Two styles cover the supported languages: property (key: value inside
// an object literal or argument list, props.go) and builder
// (.model("x").maxTokens(1024) chains, builder.go). This file holds what
// both need.

// callInfo is one parsed call expression.
type callInfo struct {
	Callee string
	Props  map[string]string // raw value text by property name, as written
}

// propertyStyle lists the languages whose calls read as key value pairs.
func propertyStyle(p string) bool {
	switch path.Ext(p) {
	case ".js", ".jsx", ".ts", ".tsx", ".mjs", ".cjs",
		".py", ".ipynb", ".rb", ".go", ".cs", ".php", ".sh", ".bash", ".zsh", ".json":
		return true
	}
	return false
}

// builderStyle lists the languages whose calls read as method chains.
func builderStyle(p string) bool {
	switch path.Ext(p) {
	case ".java", ".kt", ".kts", ".rs", ".scala", ".swift":
		return true
	}
	return false
}

var keySeparators = strings.NewReplacer("_", "", "-", "")

// normKey folds case and word separators so max_tokens, maxTokens,
// MaxTokens and the kebab case max-tokens Spring AI writes all land on
// one name.
func normKey(k string) string {
	return keySeparators.Replace(strings.ToLower(k))
}

// prop resolves a property by any of the given names, first name in the
// list winning. Within one name an exactly spelled key wins, otherwise
// the lexicographically smallest key that folds to it does, so
// duplicate spellings resolve the same way on every run.
func prop(info *callInfo, names ...string) (string, bool) {
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
	if v, ok := prop(info, names...); ok {
		if f, ok := numberFrom(v); ok {
			return f, true
		}
	}
	for _, wrapper := range []string{"config", "generationConfig", "generation_config"} {
		v, ok := prop(info, wrapper)
		if !ok {
			continue
		}
		inner := wrapperProps(v)
		if inner == nil {
			continue
		}
		wrapped := &callInfo{Props: inner}
		if raw, ok := prop(wrapped, names...); ok {
			if f, ok := numberFrom(raw); ok {
				return f, true
			}
		}
	}
	return 0, false
}

// applyCallInfo overrides the regex read shape for the fields the
// property list decides outright. Every field is set only, as effort,
// retries, dimensions and image detail already were: a wrapper the
// reader does not know, Bedrock's inferenceConfig above all, is not
// evidence that the parameter is absent, and clearing it threw away a
// cap the regex layer had already read. Schema and embedding evidence
// only widen, since both can also come from resolved references the
// properties cannot see.
func applyCallInfo(s *Shape, info *callInfo) {
	if v, ok := propNumber(info, "temperature"); ok {
		s.Temperature = &v
	}
	if v, ok := propNumber(info,
		"max_tokens", "max_output_tokens", "max_completion_tokens", "maxTokens"); ok {
		iv := int(v)
		s.MaxTokens = &iv
	}
	_, s.Tools = prop(info, "tools")
	choice, _ := prop(info, "tool_choice")
	s.ForcedTool = strings.Contains(choice, `"tool"`) || strings.Contains(choice, "'tool'")
	stream, _ := prop(info, "stream")
	// Case insensitive: Python writes True.
	s.Streaming = strings.EqualFold(strings.TrimSpace(stream), "true") ||
		strings.HasSuffix(info.Callee, ".stream") ||
		strings.Contains(info.Callee, "streamText")
	for _, k := range []string{"schema", "response_format", "responseSchema", "input_schema", "output_config"} {
		if _, ok := prop(info, k); ok {
			s.JSONSchema = true
		}
	}
	if raw, ok := prop(info, "effort", "reasoning_effort"); ok {
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
	if detail, ok := prop(info, "detail"); ok {
		d := strings.Trim(strings.TrimSpace(detail), `"'`)
		s.ImageDetailHigh = d == "high"
	}
	if strings.Contains(strings.ToLower(info.Callee), "embed") {
		s.EmbeddingCall = true
	}
}
