package scan

import (
	"regexp"
	"strings"
)

// The system prompt, tried in this order: an inline literal after
// system, an identifier after system, a system content block with a
// text field, and the chat completions style system role message.
// Anything else reports empty, which the rules read as "not visible"
// and never as "small".
var (
	reSystemInline = regexp.MustCompile("(?:\"system\"|system)\\s*[:=]\\s*([\"'`])")
	reSystemIdent  = regexp.MustCompile(`(?:"system"|system)\s*[:=]\s*([A-Za-z_][A-Za-z0-9_]*)`)
	reSystemBlock  = regexp.MustCompile(`(?:"system"|system)\s*[:=]\s*\[`)
	// A block's text may be an identifier or a literal: an inline prompt
	// is the same prompt as a named one and must measure the same.
	reTextField  = regexp.MustCompile("(?:\"text\"|text)\\s*[:=]\\s*([A-Za-z_][A-Za-z0-9_]*|[\"'`])")
	reRoleSystem = regexp.MustCompile("(?s)(?:\"role\"|role)\\s*[:=]?\\s*[\"']system[\"']\\s*,\\s*(?:\"content\"|content)\\s*[:=]\\s*([A-Za-z_][A-Za-z0-9_$]*|[\"'`])")
)

func (a *analyzer) systemPromptText(p string, r region) string {
	content := a.byPath[p]
	text := content[r.start:r.end]
	if loc := reSystemInline.FindStringSubmatchIndex(text); loc != nil {
		delim := text[loc[2]:loc[3]]
		if lit, ok := literalText(content, r.start+loc[3], delim); ok {
			return lit
		}
	}
	if m := reSystemIdent.FindStringSubmatch(text); m != nil {
		if lit, ok := a.resolveConstText(p, m[1]); ok {
			return lit
		}
	}
	if loc := reSystemBlock.FindStringIndex(text); loc != nil {
		tail := text[loc[1]:min(loc[1]+300, len(text))]
		if m := reTextField.FindStringSubmatchIndex(tail); m != nil {
			if lit, ok := a.promptValue(p, content, tail[m[2]:m[3]], r.start+loc[1]+m[3]); ok {
				return lit
			}
		}
	}
	if m := reRoleSystem.FindStringSubmatchIndex(text); m != nil {
		if lit, ok := a.promptValue(p, content, text[m[2]:m[3]], r.start+m[3]); ok {
			return lit
		}
	}
	return ""
}

// promptValue reads a prompt that the regex captured either as an
// opening delimiter, in which case the literal starts at from, or as an
// identifier to resolve.
func (a *analyzer) promptValue(p, content, value string, from int) (string, bool) {
	switch value {
	case `"`, "'", "`":
		return literalText(content, from, value)
	default:
		return a.resolveConstText(p, value)
	}
}

// literalText reads the string literal that starts right after start,
// whose opening delimiter was delim. Quotes stay on one line; backticks
// and triple quotes may span lines.
func literalText(content string, start int, delim string) (string, bool) {
	rest := content[start:]
	if delim == `"` && strings.HasPrefix(rest, `""`) {
		end := strings.Index(rest[2:], `"""`)
		if end < 0 {
			return "", false
		}
		return rest[2 : 2+end], true
	}
	if delim == "'" && strings.HasPrefix(rest, "''") {
		end := strings.Index(rest[2:], "'''")
		if end < 0 {
			return "", false
		}
		return rest[2 : 2+end], true
	}
	if delim == `"` || delim == "'" {
		// Escape aware, mirroring the masker: an escaped quote belongs
		// to the string, an unescaped newline ends it.
		q := delim[0]
		for i := 0; i < len(rest); i++ {
			switch rest[i] {
			case '\\':
				i++
			case '\n':
				return "", false
			case q:
				return rest[:i], true
			}
		}
		return "", false
	}
	end := strings.Index(rest, delim)
	if end < 0 {
		return "", false
	}
	return rest[:end], true
}

// resolveConstText finds a string constant by name, first in the same
// file and then across up to three import hops, with a cycle guard.
// Never outside the scanned repo.
func (a *analyzer) resolveConstText(p, name string) (string, bool) {
	return a.resolveConstHop(p, name, 0, map[string]bool{})
}

func (a *analyzer) resolveConstHop(p, name string, depth int, seen map[string]bool) (string, bool) {
	if depth > 3 || seen[p+"\x00"+name] {
		return "", false
	}
	seen[p+"\x00"+name] = true
	content := a.byPath[p]
	if text, ok := resolveConstIn(content, name); ok {
		return text, true
	}
	for _, target := range a.importTargets(p, name) {
		if _, ok := a.byPath[target]; ok {
			if text, ok := a.resolveConstHop(target, name, depth+1, seen); ok {
				return text, true
			}
		}
	}
	return "", false
}

func resolveConstIn(content, name string) (string, bool) {
	// The name needs a left boundary, or resolving PROMPT matches the
	// tail of LEGACY_PROMPT. Triple quotes come before their single char
	// forms so the longer delimiter wins.
	re := regexp.MustCompile(`(?m)(?:^|[^A-Za-z0-9_$])` + regexp.QuoteMeta(name) + "\\s*=\\s*(`|\"\"\"|'''|\"|')")
	m := re.FindStringSubmatchIndex(content)
	if m == nil {
		return "", false
	}
	return literalText(content, m[3], content[m[2]:m[3]])
}
