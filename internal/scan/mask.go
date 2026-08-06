package scan

import "path"

// Masked source lets the other layers reason about code without being
// fooled by prose. Two views, both offset preserving (every masked byte
// becomes a space, newlines stay):
//
//	all    blanks comments and every string interior; safe for bracket
//	       counting, since braces inside prompts no longer count.
//	prose  blanks comments and only long string interiors, so short
//	       syntax level strings like "tool" or "input_schema" survive
//	       for the shape regexes while prompt prose does not.
type masked struct {
	all   string
	prose string
}

// Strings longer than this are prose, not syntax.
const proseStringLimit = 60

type spanKind int

const (
	spanComment spanKind = iota
	spanString
)

type span struct {
	kind                       spanKind
	start, end                 int
	interiorStart, interiorEnd int
}

// langFamily describes just enough syntax to find comments and strings.
type langFamily struct {
	hashComment  bool
	slashComment bool
	blockComment bool
	backtick     bool
	triples      bool
	quotes       bool
}

func familyFor(p string) langFamily {
	switch path.Ext(p) {
	case ".py", ".ipynb":
		return langFamily{hashComment: true, triples: true, quotes: true}
	case ".sh", ".bash", ".zsh":
		// Shell strings are often the payload (curl -d '{...}'), so
		// string interiors stay visible to the shape layer.
		return langFamily{hashComment: true}
	case ".js", ".jsx", ".ts", ".tsx", ".mjs", ".cjs":
		return langFamily{slashComment: true, blockComment: true, backtick: true, quotes: true}
	case ".go":
		return langFamily{slashComment: true, blockComment: true, backtick: true, quotes: true}
	case ".java", ".kt", ".c", ".h", ".cpp", ".cc", ".cs", ".php", ".scala", ".swift":
		return langFamily{slashComment: true, blockComment: true, quotes: true}
	case ".md", ".markdown":
		return langFamily{}
	case ".json":
		return langFamily{quotes: true}
	default:
		// env files, yaml, toml, shell, ruby, requirements
		return langFamily{hashComment: true, quotes: true}
	}
}

func maskFile(p, content string) masked {
	spans := scanSpans(content, familyFor(p))
	all := []byte(content)
	prose := []byte(content)
	for _, s := range spans {
		switch s.kind {
		case spanComment:
			blank(all, s.start, s.end)
			blank(prose, s.start, s.end)
		case spanString:
			blank(all, s.interiorStart, s.interiorEnd)
			if s.interiorEnd-s.interiorStart > proseStringLimit {
				blank(prose, s.interiorStart, s.interiorEnd)
			}
		}
	}
	return masked{all: string(all), prose: string(prose)}
}

func blank(b []byte, from, to int) {
	for i := from; i < to && i < len(b); i++ {
		if b[i] != '\n' {
			b[i] = ' '
		}
	}
}

func scanSpans(s string, fam langFamily) []span {
	var spans []span
	i := 0
	for i < len(s) {
		c := s[i]
		switch {
		case fam.triples && c == '"' && hasAt(s, i, `"""`):
			end := findClose(s, i+3, `"""`, false)
			spans = append(spans, span{spanString, i, end, i + 3, end - 3})
			i = end
		case fam.triples && c == '\'' && hasAt(s, i, "'''"):
			end := findClose(s, i+3, "'''", false)
			spans = append(spans, span{spanString, i, end, i + 3, end - 3})
			i = end
		case fam.backtick && c == '`':
			end := findClose(s, i+1, "`", false)
			spans = append(spans, span{spanString, i, end, i + 1, end - 1})
			i = end
		case fam.quotes && (c == '"' || c == '\''):
			end := quoteEnd(s, i)
			spans = append(spans, span{spanString, i, end, i + 1, max(i+1, end-1)})
			i = end
		case fam.slashComment && hasAt(s, i, "//"):
			end := lineEnd(s, i)
			spans = append(spans, span{kind: spanComment, start: i, end: end})
			i = end
		case fam.blockComment && hasAt(s, i, "/*"):
			end := findClose(s, i+2, "*/", true)
			spans = append(spans, span{kind: spanComment, start: i, end: end})
			i = end
		case fam.hashComment && c == '#':
			end := lineEnd(s, i)
			spans = append(spans, span{kind: spanComment, start: i, end: end})
			i = end
		default:
			i++
		}
	}
	return spans
}

func hasAt(s string, i int, sub string) bool {
	return i+len(sub) <= len(s) && s[i:i+len(sub)] == sub
}

func lineEnd(s string, i int) int {
	for ; i < len(s); i++ {
		if s[i] == '\n' {
			return i
		}
	}
	return len(s)
}

// findClose scans for the closing marker, honoring backslash escapes
// unless the marker belongs to a comment.
func findClose(s string, from int, close string, comment bool) int {
	for i := from; i < len(s); i++ {
		if !comment && s[i] == '\\' {
			i++
			continue
		}
		if hasAt(s, i, close) {
			return i + len(close)
		}
	}
	return len(s)
}

// quoteEnd ends a single line string at its quote, an unescaped newline,
// or end of input.
func quoteEnd(s string, open int) int {
	q := s[open]
	for i := open + 1; i < len(s); i++ {
		switch s[i] {
		case '\\':
			i++
		case '\n':
			return i
		case q:
			return i + 1
		}
	}
	return len(s)
}
