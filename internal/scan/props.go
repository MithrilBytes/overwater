package scan

import (
	"regexp"
	"strings"
)

func isChainChar(b byte) bool {
	return b == '.' || b == '_' || b == '$' ||
		(b >= '0' && b <= '9') || (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}

func isWS(b byte) bool {
	return b == ' ' || b == '\t' || b == '\n' || b == '\r'
}

// parseCall reads the call whose argument extent spans [objStart,
// objEnd), either an object literal or the call's own parenthesized
// arguments. Structure comes from the all view so braces in strings
// cannot mislead it; values come from the original content so prompt
// literals stay intact.
func parseCall(content string, src maskedFile, objStart, objEnd int) *callInfo {
	if objStart >= len(content) {
		return nil
	}
	open := content[objStart]
	if open != '{' && open != '(' {
		return nil
	}
	info := &callInfo{Props: parseProps(content, src.all, src.prose, objStart+1, objEnd-1)}
	info.Callee = calleeChain(src.all, objStart)
	return info
}

// calleeChain walks left from the extent to the identifier chain
// calling it, crossing whitespace only across a dot so "await client"
// never glues into one name.
func calleeChain(all string, objStart int) string {
	if all[objStart] == '(' {
		return chainBefore(all, objStart-1)
	}
	i := objStart - 1
	for i >= 0 && isWS(all[i]) {
		i--
	}
	if i < 0 || all[i] != '(' {
		return ""
	}
	return chainBefore(all, i-1)
}

func chainBefore(all string, from int) string {
	i := from
	var rev []byte
	for i >= 0 {
		c := all[i]
		if isChainChar(c) {
			rev = append(rev, c)
			i--
			continue
		}
		if isWS(c) {
			j := i
			for j >= 0 && isWS(all[j]) {
				j--
			}
			if j >= 0 && isChainChar(all[j]) &&
				(all[j] == '.' || (len(rev) > 0 && rev[len(rev)-1] == '.')) {
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
// list. Separators are ':' and '=', guarded against comparisons,
// arrows, and walrus assignments. Depth counting reads the all view,
// key names the prose view, values the original content.
func parseProps(content, all, prose string, start, end int) map[string]string {
	props := map[string]string{}
	if start < 0 || end > len(all) || start >= end {
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
		c := all[i]
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
				next := all[i+1]
				if next == '=' || next == '>' || next == ':' {
					continue
				}
			}
			if i > start {
				prev := all[i-1]
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

// A wrapper value that is a constructor or function call: Name(...) or
// pkg.Name(...). Google GenAI's typed config is written this way, and
// its keyword arguments are the call's parameters exactly as an object
// literal's entries are.
var reConstructorCall = regexp.MustCompile(`^[A-Za-z_$][A-Za-z0-9_$]*(?:\s*\.\s*[A-Za-z_$][A-Za-z0-9_$]*)*\s*\(`)

// wrapperProps reads the properties out of a config wrapper value,
// object literal or typed constructor call. Nil means it is neither and
// the caller leaves the wrapper alone.
func wrapperProps(value string) map[string]string {
	v := strings.TrimSpace(value)
	open := 0
	if !strings.HasPrefix(v, "{") {
		loc := reConstructorCall.FindStringIndex(v)
		if loc == nil {
			return nil
		}
		open = loc[1] - 1
	}
	src := maskFile("w.ts", v)
	closer, ok := matchClose(src.all, open)
	if !ok {
		return nil
	}
	return parseProps(v, src.all, src.prose, open+1, closer)
}
