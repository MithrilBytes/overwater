package scan

import (
	"regexp"
	"sort"
	"strings"

	"github.com/MithrilBytes/overwater/catalog"
)

// nameChar reports whether b can appear inside a model name; anything
// else marks a word boundary.
func nameChar(b byte) bool {
	return b == '.' || b == '_' || b == '-' ||
		(b >= '0' && b <= '9') || (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}

// Model looking strings that are not in the catalog get reported at low
// confidence instead of being ignored.
var unknownModelRE = regexp.MustCompile(
	`\b(?:gpt-[0-9o][\w.-]*` +
		`|claude-[\w.-]+` +
		`|gemini-[\w.-]+` +
		`|mistral-[\w.-]+` +
		`|command-[\w.-]+` +
		`|text-embedding-[\w.-]+` +
		`|voyage-[\w.-]+` +
		`|llama-[\w.-]+)`)

// findModelRefs scans one file for catalog ids and aliases (layer 2).
// The catalog doubles as the detection dictionary.
func findModelRefs(relPath string, data []byte, names map[string]*catalog.Model) []Site {
	keys := make([]string, 0, len(names))
	for k := range names {
		keys = append(keys, k)
	}
	// Longest first, so a dated alias claims its span before the bare id
	// that prefixes it.
	sort.Slice(keys, func(i, j int) bool {
		if len(keys[i]) != len(keys[j]) {
			return len(keys[i]) > len(keys[j])
		}
		return keys[i] < keys[j]
	})

	var sites []Site
	for lineNo, line := range strings.Split(string(data), "\n") {
		type claim struct{ start, end int }
		var claimed []claim
		overlaps := func(s, e int) bool {
			for _, c := range claimed {
				if s < c.end && e > c.start {
					return true
				}
			}
			return false
		}
		for _, key := range keys {
			for idx := 0; ; {
				i := strings.Index(line[idx:], key)
				if i < 0 {
					break
				}
				start := idx + i
				end := start + len(key)
				idx = end
				if start > 0 && nameChar(line[start-1]) {
					continue
				}
				if end < len(line) && nameChar(line[end]) {
					continue
				}
				if overlaps(start, end) {
					continue
				}
				if !shortIDInContext(line, key, start, end) {
					continue
				}
				claimed = append(claimed, claim{start, end})
				sites = append(sites, Site{
					File:    relPath,
					Line:    lineNo + 1,
					Col:     start,
					Ref:     key,
					ModelID: names[key].ID,
					Known:   true,
				})
			}
		}
		for _, loc := range unknownModelRE.FindAllStringIndex(line, -1) {
			if overlaps(loc[0], loc[1]) {
				continue
			}
			// RE2 counts a hyphen as a non word character, so \b matches
			// in the middle of a hyphenated identifier and the pattern
			// finds a model inside sk-proj-command-secret or
			// my-claude-code-plugin. The catalog path above already
			// guards its left edge with nameChar; this one has to as
			// well, or every hyphenated name that happens to contain a
			// family prefix becomes a call site.
			if loc[0] > 0 && nameChar(line[loc[0]-1]) {
				continue
			}
			ref := strings.TrimRight(line[loc[0]:loc[1]], ".-")
			sites = append(sites, Site{
				File:  relPath,
				Line:  lineNo + 1,
				Col:   loc[0],
				Ref:   ref,
				Known: false,
			})
		}
	}
	return sites
}

// shortIDMaxLen is the length below which a catalog id is too generic to
// stand on its own. Only "o3" qualifies today, and it collides with the
// variable names a JavaScript minifier produces, which is how a
// committed Storybook bundle came to report five o3 call sites.
const shortIDMaxLen = 3

// shortIDInContext reports whether a very short id appears somewhere a
// model id plausibly appears: inside quotes, or as the value of a key
// that names a model. Longer ids carry their own evidence and are always
// accepted.
func shortIDInContext(line, key string, start, end int) bool {
	if len(key) > shortIDMaxLen {
		return true
	}
	quote := func(b byte) bool { return b == '"' || b == '\'' || b == '`' }
	if start > 0 && end < len(line) && quote(line[start-1]) && quote(line[end]) {
		return true
	}
	// model: o3, model = o3, "model":o3 - the unquoted config spelling.
	before := strings.TrimRight(line[:start], " \t\"'`:=>,([")
	if i := strings.LastIndexAny(before, " \t\"'`{,([<"); i >= 0 {
		before = before[i+1:]
	}
	return modelKeyish(before)
}
