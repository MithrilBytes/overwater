package scan

import (
	"regexp"
	"sort"
	"strings"

	"github.com/MithrilBytes/overwater/catalog"
)

// lowerASCII lowercases a and z only, so every byte offset in the result
// still points at the same byte of the input. strings.ToLower cannot be
// used for this: some Unicode letters change width when folded, and a
// prompt full of them would shift every column the matcher reports.
func lowerASCII(s string) string {
	b := []byte(s)
	for i := range b {
		if b[i] >= 'A' && b[i] <= 'Z' {
			b[i] += 'a' - 'A'
		}
	}
	return string(b)
}

// nameChar reports whether b can appear inside a model name; anything
// else marks a word boundary.
func nameChar(b byte) bool {
	return b == '.' || b == '_' || b == '-' ||
		(b >= '0' && b <= '9') || (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}

// Model looking strings that are not in the catalog get reported at low
// confidence instead of being ignored.
//
// The families here are the ones the catalog already ships, because the
// case that matters is a provider shipping a version we have not added
// yet: a repository pinning deepseek-v4-flash used to produce nothing at
// all, since the catalog carries only deepseek-chat and
// deepseek-reasoner and the pattern had no deepseek branch. Low
// confidence and no price beats silence.
//
// Two catalog families are deliberately absent. "nova" and "embed" are
// ordinary English words that begin ordinary identifiers, and a pattern
// is only worth having if it costs less noise than it catches. The
// short ids o3 and o4 are handled by shortIDInContext instead, which can
// require the quoting these cannot.
var unknownModelRE = regexp.MustCompile(
	`(?i)\b(?:gpt-[0-9o][\w.-]*` +
		`|claude-[\w.-]+` +
		`|gemini-[\w.-]+` +
		`|mistral-[\w.-]+` +
		`|command-[\w.-]+` +
		`|text-embedding-[\w.-]+` +
		`|voyage-[\w.-]+` +
		`|llama-[\w.-]+` +
		`|deepseek-[\w.-]+` +
		`|qwen-?[0-9][\w.-]*` +
		`|grok-[\w.-]+` +
		`|kimi-[\w.-]+` +
		`|glm-[\w.-]+` +
		`|codestral-[\w.-]+` +
		`|magistral-[\w.-]+` +
		`|ministral-[\w.-]+)`)

// Agent tooling borrows the family name of the model it drives. A
// Claude Code plugin manifest, a gemini-cli hook and a claude-templates
// directory are not models, and a repository full of them used to report
// dozens of sites: demarkus, an MCP broker with no LLM dependency at
// all, produced 37.
var toolNameRE = regexp.MustCompile(
	`(?i)^(?:claude|gemini|gpt|grok|llama|mistral)-` +
		`(?:code|cli|desktop|app|plugin|template|templates|hook|hooks|` +
		`pre|post|bot|agent|sdk|api|key|token|proxy|ui|web|server|mcp)` +
		`(?:[-.].*)?$`)

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
		// Providers ship mixed case ids (Qwen3-VL-235B-A22B-Instruct) and
		// people write GPT-4o. Catalog keys are lowercase, so matching
		// happens against a lowercase view whose offsets are the line's.
		lower := lowerASCII(line)
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
				i := strings.Index(lower[idx:], key)
				if i < 0 {
					break
				}
				start := idx + i
				end := start + len(key)
				idx = end
				if start > 0 && nameChar(lower[start-1]) {
					continue
				}
				if end < len(lower) && nameChar(lower[end]) {
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
					File: relPath,
					Line: lineNo + 1,
					Col:  start,
					// The catalog key, not the source spelling: the rules
					// engine resolves a site by looking Ref up in the same
					// map, so GPT-4o has to arrive as gpt-4o or it reads
					// as a model nobody has heard of.
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
			if toolNameRE.MatchString(ref) {
				continue
			}
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
