package scan

import (
	"regexp"
	"strings"
)

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
