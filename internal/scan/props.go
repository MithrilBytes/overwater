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

// A wrapper value that is a constructor or function call: Name(...) or
// pkg.Name(...). Google GenAI's typed config is written this way, and
// its keyword arguments are the call's parameters exactly as an object
// literal's entries are.
var reConstructorCall = regexp.MustCompile(`^[A-Za-z_$][A-Za-z0-9_$]*(?:\s*\.\s*[A-Za-z_$][A-Za-z0-9_$]*)*\s*\(`)

// wrapperProps reads the properties out of a config wrapper value,
// whether it is an object literal or a typed constructor call. Nil means
// the value is neither, and the caller leaves the wrapper alone.
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
	m := maskFile("w.ts", v)
	closer, ok := matchClose(m.all, open)
	if !ok {
		return nil
	}
	return parseProps(v, m.all, m.prose, open+1, closer)
}
