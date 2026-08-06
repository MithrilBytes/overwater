package scan

import (
	"regexp"
	"strings"
)

// Call extents replace fixed line windows: from the model string hit,
// ascend bracket levels until the enclosing region names a model
// parameter. That region is the call's own arguments, so signals from a
// neighboring call site can no longer bleed in. Bracket counting runs
// on the fully masked source, where braces inside prompts do not exist.
const (
	extentAscendMax = 8
	extentMaxBytes  = 16000
)

var reModelKey = regexp.MustCompile(`(?:^|[^A-Za-z0-9_"])(?:"model"|model)\s*[:=]`)

func callExtent(maskedAll, maskedProse string, hit int) (int, int, bool) {
	pos := hit
	for level := 0; level < extentAscendMax; level++ {
		open, ok := enclosingOpen(maskedAll, pos)
		if !ok {
			return 0, 0, false
		}
		closer, ok := matchClose(maskedAll, open)
		if !ok || closer <= hit {
			return 0, 0, false
		}
		if reModelKey.MatchString(maskedProse[open : closer+1]) {
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

// headExpand pulls the region start up two lines above the opener so
// call names like streamText( or .stream( stay visible to the shape
// regexes.
func headExpand(content string, from int) int {
	i := from
	for k := 0; k < 3; k++ {
		nl := strings.LastIndexByte(content[:i], '\n')
		if nl < 0 {
			return 0
		}
		i = nl
	}
	return i + 1
}

// Function definition shapes across the supported languages, used to
// find the name of the function enclosing a call site.
var reFuncDefs = []*regexp.Regexp{
	regexp.MustCompile(`(?:function|func|def)\s+([A-Za-z_][A-Za-z0-9_]*)`),
	regexp.MustCompile(`(?:const|let|var)\s+([A-Za-z_][A-Za-z0-9_]*)\s*=\s*(?:async\b|\()`),
}

// enclosingFuncName returns the last function name defined shortly
// before the region, which is almost always the function the call lives
// in. It reads masked prose, so names inside prompts do not qualify.
func enclosingFuncName(prose string, before int) string {
	from := max(0, before-2000)
	region := prose[from:before]
	best := ""
	bestPos := -1
	for _, re := range reFuncDefs {
		for _, m := range re.FindAllStringSubmatchIndex(region, -1) {
			if m[0] > bestPos {
				bestPos = m[0]
				best = region[m[2]:m[3]]
			}
		}
	}
	return best
}
