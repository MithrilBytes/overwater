package scan

import (
	"regexp"
	"strings"
)

// An extent is the bracket region around a model string that names a
// model parameter, which is the call's own argument list: signals from a
// neighboring call site cannot bleed in. Bracket counting runs on the
// all view, where braces inside prompts do not exist.
const (
	extentAscendMax = 8
	extentMaxBytes  = 16000
	// lookbackMaxBytes bounds every "n lines above" hop. A minified file
	// is one enormous line, so an unbounded hop reads back to byte zero
	// and scan cost turns quadratic in references per file.
	lookbackMaxBytes = 2000
)

// Case insensitive so Go's Model: and C#'s Model = count too.
var reModelKey = regexp.MustCompile(`(?i)(?:^|[^A-Za-z0-9_"])(?:"model"|model)\s*[:=]`)

func callExtent(all, prose string, hit int) (int, int, bool) {
	pos := hit
	for level := 0; level < extentAscendMax; level++ {
		open, ok := enclosingOpen(all, pos)
		if !ok {
			return 0, 0, false
		}
		closer, ok := matchClose(all, open)
		if !ok || closer <= hit {
			return 0, 0, false
		}
		if reModelKey.MatchString(prose[open : closer+1]) {
			return open, closer + 1, true
		}
		pos = open
	}
	return 0, 0, false
}

// enclosingOpen walks left from a position to the nearest bracket that
// is still open there.
func enclosingOpen(s string, from int) (int, bool) {
	depth := 0
	for i := from - 1; i >= 0; i-- {
		switch s[i] {
		case ')', ']', '}':
			depth++
		case '(', '[', '{':
			if depth == 0 {
				return i, true
			}
			depth--
		}
	}
	return 0, false
}

// matchClose finds the closer matching the bracket at open, bailing on
// malformed nesting or oversized extents.
func matchClose(s string, open int) (int, bool) {
	var want byte
	switch s[open] {
	case '(':
		want = ')'
	case '[':
		want = ']'
	case '{':
		want = '}'
	default:
		return 0, false
	}
	depth := 0
	limit := min(len(s), open+extentMaxBytes)
	for i := open + 1; i < limit; i++ {
		switch s[i] {
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			if depth == 0 {
				if s[i] == want {
					return i, true
				}
				return 0, false
			}
			depth--
		}
	}
	return 0, false
}

// innermostExtent returns the closest balanced bracket region around
// the hit, with no requirement that it name a model parameter. The
// fallback for wrapper calls, where the model string is an argument to
// a helper rather than a keyed property.
func innermostExtent(all string, hit int) (int, int, bool) {
	open, ok := enclosingOpen(all, hit)
	if !ok {
		return 0, 0, false
	}
	closer, ok := matchClose(all, open)
	if !ok || closer <= hit {
		return 0, 0, false
	}
	return open, closer + 1, true
}

// headExpand pulls the region start a couple of lines above the opener
// so call names like streamText( or .stream( stay visible to the shape
// regexes, never reaching back further than lookbackMaxBytes.
func headExpand(content string, from int) int {
	limit := max(0, from-lookbackMaxBytes)
	i := from
	for k := 0; k < 3; k++ {
		nl := strings.LastIndexByte(content[limit:i], '\n')
		if nl < 0 {
			return limit
		}
		i = limit + nl
	}
	return i + 1
}

// The fallback window, used when no extent can be found: config files,
// env files, languages the extent walkers do not understand. Bounded in
// lines and, for minified files where a line is not a useful unit, in
// bytes on each side of the hit.
const (
	windowLines    = 30
	windowMaxBytes = 2000
)

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
	src := a.masked(p)
	extent := func(s, e int) region {
		return region{start: headExpand(content, s), end: e, extentStart: s, isExtent: true, hit: hit}
	}
	if builderStyle(p) {
		if s, e, ok := builderExtent(src.all, hit); ok {
			return extent(s, e)
		}
	}
	if s, e, ok := callExtent(src.all, src.prose, hit); ok {
		return extent(s, e)
	}
	if s, e, ok := innermostExtent(src.all, hit); ok {
		return extent(s, e)
	}
	s, e := windowBounds(content, a.lineStarts(p), line, hit)
	return region{start: s, end: e, hit: hit}
}

// windowBounds returns the fallback window around the hit at a one
// based line number. Without the byte bound every reference in a
// minified file reads the whole file, which is quadratic.
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

// Function definition shapes across the supported languages. The third
// form catches C family methods (name(args) {), which have no keyword;
// the stopwords keep control statements from posing as names.
var reFuncDefs = []*regexp.Regexp{
	// Keywords need a left word boundary so "undef foo" is not a def.
	regexp.MustCompile(`\b(?:function|func|def|fn)\s+([A-Za-z_][A-Za-z0-9_]*)`),
	regexp.MustCompile(`\b(?:const|let|var)\s+([A-Za-z_][A-Za-z0-9_]*)\s*=\s*(?:async\b|\()`),
	regexp.MustCompile(`([A-Za-z_][A-Za-z0-9_]*)\s*\([^()]*\)\s*\{`),
}

var funcNameStopwords = map[string]bool{
	"if": true, "for": true, "while": true, "switch": true, "catch": true,
	"return": true, "foreach": true, "using": true, "when": true,
	"unless": true, "match": true, "lock": true, "defer": true,
}

// enclosingFuncName returns the last function name defined shortly
// before the offset, which is almost always the function the call lives
// in. It reads the prose view, so names inside prompts do not qualify.
func enclosingFuncName(prose string, before int) string {
	from := max(0, before-2000)
	text := prose[from:before]
	best := ""
	bestPos := -1
	for _, re := range reFuncDefs {
		for _, m := range re.FindAllStringSubmatchIndex(text, -1) {
			name := text[m[2]:m[3]]
			if funcNameStopwords[name] {
				continue
			}
			if m[0] > bestPos {
				bestPos = m[0]
				best = name
			}
		}
	}
	return best
}
