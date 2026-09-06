package scan

import (
	"path"
	"strings"
)

// Masked source lets the other layers reason about code without being
// fooled by prose. Three views, all offset preserving (every masked byte
// becomes a space, newlines stay):
//
//	all    blanks comments and every string interior; safe for bracket
//	       counting, since braces inside prompts no longer count.
//	prose  blanks comments and only long string interiors, so short
//	       syntax level strings like "tool" or "input_schema" survive
//	       for the shape regexes while prompt prose does not.
//	code   blanks comments and multi line string interiors, keeping
//	       single line strings whole; this is where layer 2 looks for
//	       model ids, which live in strings but never in a docstring.
//
// The first two are built together and held for the whole pass, since
// layers 3 and 4 come back to them per call site. The code view is
// built by itself, by maskCode, because layer 2 reads it once per file
// and nothing reads it again; see analyzer.codeView.
type maskedFile struct {
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
	rawBacktick  bool // backtick strings take backslash literally (Go)
	rustRaw      bool // r#"..."# strings, where the hash is a delimiter (Rust)
	heredoc      bool // <<~ID blocks, whose body starts on the line after the opener (Ruby)
	verbatim     bool // @"..." strings that span lines and double a quote to escape it (C#)
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
	case ".js", ".jsx", ".ts", ".tsx", ".mjs", ".cjs", ".vue", ".svelte", ".astro":
		// Single file components are a script block in a template, and
		// the script is the part that calls a model.
		return langFamily{slashComment: true, blockComment: true, backtick: true, quotes: true}
	case ".go":
		return langFamily{slashComment: true, blockComment: true, backtick: true, rawBacktick: true, quotes: true}
	case ".rs":
		return langFamily{slashComment: true, blockComment: true, rustRaw: true, quotes: true}
	case ".rb", ".rake":
		// Ruby writes its prompts as heredocs. Read as code, a body is
		// invisible to the prompt reader, and its #{} interpolations
		// blank the rest of each line as comments.
		return langFamily{hashComment: true, quotes: true, heredoc: true}
	case ".java", ".kt", ".kts":
		// Java text blocks and Kotlin raw strings spell a block of prose
		// the way Python does. Without the delimiter the block scans as
		// code, every line of it, the braces in its templates included.
		return langFamily{slashComment: true, blockComment: true, triples: true, quotes: true}
	case ".swift", ".scala", ".gradle", ".groovy":
		// Swift multi line strings and Scala and Groovy triple quoted
		// strings are blocks of prose, the way Python's are. In the C
		// family they scanned as a one line string that ended at the
		// first newline, the rest of the prompt reading as code.
		return langFamily{slashComment: true, blockComment: true, triples: true, quotes: true}
	case ".cs":
		// C# 11 raw strings are triple quoted; verbatim @"..." strings
		// span lines too, and escape a quote by doubling it.
		return langFamily{slashComment: true, blockComment: true, triples: true, verbatim: true, quotes: true}
	case ".c", ".h", ".cpp", ".cc", ".php":
		return langFamily{slashComment: true, blockComment: true, quotes: true}
	case ".tf", ".tfvars", ".hcl":
		// HCL takes both comment spellings, so neither family alone.
		return langFamily{hashComment: true, slashComment: true, blockComment: true, quotes: true}
	case ".md", ".markdown":
		return langFamily{}
	case ".json":
		return langFamily{quotes: true}
	default:
		// env files, yaml, toml, shell, ruby, requirements
		return langFamily{hashComment: true, quotes: true}
	}
}

func maskFile(p, content string) maskedFile {
	return maskViews(content, scanSpans(content, familyFor(p)))
}

// maskViews builds the two whole pass views from a span list already
// scanned, so a caller that wants the code view as well pays for one
// scan rather than two.
func maskViews(content string, spans []span) maskedFile {
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
	return maskedFile{all: string(all), prose: string(prose)}
}

// maskCode builds layer 2's view: comments blanked, strings left whole
// unless they span lines.
//
// A string that spans lines is a docstring, a heredoc or a block of
// prose, and a model named inside one is being written about rather
// than called. A Python docstring is a comment in every way except
// syntactically, and used to be the one spelling of a comment layer 2
// still read.
//
// The test is the newline, not the length: the value that must survive
// is a REST endpoint naming the model in its path, which is long but
// never wraps. That is also why neither of the other two views can
// stand in for this one, since all blanks every string interior and
// prose blanks the long ones.
func maskCode(content string, spans []span) string {
	code := []byte(content)
	for _, s := range spans {
		switch s.kind {
		case spanComment:
			blank(code, s.start, s.end)
		case spanString:
			if strings.Contains(content[s.interiorStart:s.interiorEnd], "\n") {
				blank(code, s.interiorStart, s.interiorEnd)
			}
		}
	}
	return string(code)
}

func blank(b []byte, from, to int) {
	for i := from; i < to && i < len(b); i++ {
		if b[i] != '\n' {
			b[i] = ' '
		}
	}
}

// jsonStripComments blanks // and /* */ comments so JSONC configs like
// tsconfig.json still parse. Strings are respected, offsets preserved.
func jsonStripComments(s string) string {
	b := []byte(s)
	for _, sp := range scanSpans(s, langFamily{slashComment: true, blockComment: true, quotes: true}) {
		if sp.kind == spanComment {
			blank(b, sp.start, sp.end)
		}
	}
	return string(b)
}

func scanSpans(s string, fam langFamily) []span {
	var spans []span
	// A heredoc body starts on the line after its opener, so the opener
	// is noted and the rest of its line scanned as usual; the bodies are
	// consumed, in opener order, at the newline that ends that line.
	var pending []heredocOpener
	i := 0
	for i < len(s) {
		c := s[i]
		switch {
		case fam.heredoc && c == '<' && hasAt(s, i, "<<"):
			h, n := heredocOpen(s, i)
			if n == 0 {
				i++
				break
			}
			pending = append(pending, h)
			i += n
		case fam.heredoc && c == '\n' && len(pending) > 0:
			body := i + 1
			for _, h := range pending {
				sp := heredocSpan(s, body, h)
				spans = append(spans, sp)
				body = min(sp.end+1, len(s))
				i = sp.end
			}
			pending = pending[:0]
		case fam.triples && c == '"' && hasAt(s, i, `"""`):
			end, closed := findClose(s, i+3, `"""`, false)
			spans = append(spans, stringSpan(i, end, 3, closed))
			i = end
		case fam.triples && c == '\'' && hasAt(s, i, "'''"):
			end, closed := findClose(s, i+3, "'''", false)
			spans = append(spans, stringSpan(i, end, 3, closed))
			i = end
		case fam.backtick && c == '`':
			end, closed := findClose(s, i+1, "`", fam.rawBacktick)
			spans = append(spans, stringSpan(i, end, 1, closed))
			i = end
		case fam.rustRaw && (c == 'r' || c == 'b'):
			open, close := rustRawDelims(s, i)
			if open == 0 {
				i++
				break
			}
			end, closed := findClose(s, i+open, close, true)
			spans = append(spans, rawStringSpan(i, end, open, len(close), closed))
			i = end
		case fam.verbatim && c == '@' && hasAt(s, i, "@\""):
			end, closed := verbatimEnd(s, i)
			spans = append(spans, rawStringSpan(i, end, 2, 1, closed))
			i = end
		case fam.quotes && (c == '"' || c == '\''):
			end, closed := quoteEnd(s, i)
			spans = append(spans, stringSpan(i, end, 1, closed))
			i = end
		case fam.slashComment && hasAt(s, i, "//"):
			end := lineEnd(s, i)
			spans = append(spans, span{kind: spanComment, start: i, end: end})
			i = end
		case fam.blockComment && hasAt(s, i, "/*"):
			end, _ := findClose(s, i+2, "*/", true)
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

// heredocOpener is one <<ID, <<-ID or <<~ID seen on the current line.
// indented is true for the last two forms, whose terminator may be
// indented.
type heredocOpener struct {
	id       string
	indented bool
}

// heredocOpen reads a heredoc opener at i, returning its length, or zero
// when the << is a shift or an append. A bare <<ID needs an uppercase
// identifier, which is the convention that keeps it apart from
// `list <<item`; the squiggly and dash forms are unambiguous and take
// any identifier, quoted or not.
func heredocOpen(s string, i int) (heredocOpener, int) {
	j := i + 2
	var h heredocOpener
	if j < len(s) && (s[j] == '~' || s[j] == '-') {
		h.indented = true
		j++
	}
	var quote byte
	if j < len(s) && (s[j] == '\'' || s[j] == '"' || s[j] == '`') {
		quote = s[j]
		j++
	}
	start := j
	for j < len(s) && isIdentChar(s[j]) {
		j++
	}
	h.id = s[start:j]
	if h.id == "" || (s[start] >= '0' && s[start] <= '9') {
		return h, 0
	}
	if quote != 0 {
		if j >= len(s) || s[j] != quote {
			return h, 0
		}
		j++
	} else if !h.indented && strings.ToUpper(h.id) != h.id {
		return h, 0
	}
	return h, j - i
}

// heredocSpan reads the body that starts at body up to its terminator
// line, or to end of input when there is none. The span starts at the
// body rather than at the opener so the span list stays ordered; the
// terminator line is the span's only non interior part.
func heredocSpan(s string, body int, h heredocOpener) span {
	for ls := body; ls < len(s); {
		le := lineEnd(s, ls)
		line := s[ls:le]
		if h.indented {
			line = strings.TrimLeft(line, " \t")
		}
		if strings.TrimRight(line, " \t\r") == h.id {
			return span{spanString, body, le, body, ls}
		}
		ls = le + 1
	}
	return span{spanString, body, len(s), body, len(s)}
}

func lineEnd(s string, i int) int {
	for ; i < len(s); i++ {
		if s[i] == '\n' {
			return i
		}
	}
	return len(s)
}

// stringSpan builds a string span whose interior excludes the opening
// delimiter and, only when one was found, the closing delimiter.
// Interiors are clamped so start <= interiorStart <= interiorEnd <= end
// always holds, even for strings truncated at end of input.
func stringSpan(start, end, delim int, closed bool) span {
	return rawStringSpan(start, end, delim, delim, closed)
}

// rawStringSpan is stringSpan for a literal whose opening delimiter
// carries a prefix the closing one does not, which of these languages
// only Rust's r#"..."# does.
func rawStringSpan(start, end, open, close int, closed bool) span {
	is := min(start+open, end)
	ie := end
	if closed {
		ie = end - close
	}
	if ie < is {
		ie = is
	}
	return span{spanString, start, end, is, ie}
}

// verbatimEnd finds the end of a C# @"..." string opened at i. A
// doubled quote is an escaped one and a backslash is literal, so
// neither the escape aware nor the raw scanner fits it.
func verbatimEnd(s string, i int) (end int, closed bool) {
	for j := i + 2; j < len(s); j++ {
		if s[j] != '"' {
			continue
		}
		if j+1 < len(s) && s[j+1] == '"' {
			j++
			continue
		}
		return j + 1, true
	}
	return len(s), false
}

// rustRawDelims reports the opening and closing delimiters of a Rust
// raw string at i, r"...", r#"..."# and their br byte string forms, or
// a zero length opener when there is none. The hash count is chosen
// per literal and belongs to both ends, so the caller needs the closing
// marker itself rather than a length.
func rustRawDelims(s string, i int) (open int, close string) {
	j := i
	if s[j] == 'b' {
		j++
	}
	if j >= len(s) || s[j] != 'r' {
		return 0, ""
	}
	hashes := 0
	for j++; j < len(s) && s[j] == '#'; j++ {
		hashes++
	}
	if j >= len(s) || s[j] != '"' {
		return 0, ""
	}
	return j + 1 - i, `"` + strings.Repeat("#", hashes)
}

// findClose scans for the closing marker and reports whether it was
// found; unterminated spans end at len(s). Backslash escapes are
// honored unless noEscape is set, which comments and raw backtick
// strings (Go), where a backslash is a literal byte, require.
func findClose(s string, from int, close string, noEscape bool) (int, bool) {
	for i := from; i < len(s); i++ {
		if !noEscape && s[i] == '\\' {
			i++
			continue
		}
		if hasAt(s, i, close) {
			return i + len(close), true
		}
	}
	return len(s), false
}

// quoteEnd ends a single line string at its quote, an unescaped newline,
// or end of input, reporting whether the closing quote itself was found.
func quoteEnd(s string, open int) (int, bool) {
	q := s[open]
	for i := open + 1; i < len(s); i++ {
		switch s[i] {
		case '\\':
			i++
		case '\n':
			return i, false
		case q:
			return i + 1, true
		}
	}
	return len(s), false
}
