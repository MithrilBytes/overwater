package scan

import (
	"strings"
	"testing"
)

// Hostile inputs must never build a span whose interior is inverted or
// out of bounds; nearbyStrings slices content by these fields.
func TestScanSpansInvariantsOnHostileInputs(t *testing.T) {
	inputs := map[string]string{
		"triple-at-eof.py":       "m = \"gpt-4o\"\nx = \"\"\"a\n",
		"triple-exact-eof.py":    "x = \"\"\"a",
		"triple-single-eof.py":   "'''ab",
		"lone-backtick-eof.ts":   "`",
		"unterminated-quote.ts":  "\"abc",
		"quote-then-newline.js":  "y = \"abc\nz = 1\n",
		"empty-triple-eof.py":    "x = \"\"\"",
		"backtick-then-text.ts":  "`ab",
		"escape-at-eof.py":       "s = \"ab\\",
		"only-quotes.json":       "\"\"\"",
		"comment-unclosed.ts":    "/* never closed",
		"raw-backslash-eof.go":   "var p = `a\\",
		"quote-single-at-eof.py": "s = 'x",
		"triple-single-inner.py": "a = '''body",
		"crlf-unterminated.js":   "x = \"abc\r\n",
		"nested-hostility.py":    "m = \"gpt-4o\"\n'''\nx\n",
	}
	for p, content := range inputs {
		for _, s := range scanSpans(content, familyFor(p)) {
			if !(0 <= s.start && s.start <= s.interiorStart &&
				s.interiorStart <= s.interiorEnd &&
				s.interiorEnd <= s.end && s.end <= len(content)) {
				t.Errorf("%s: span invariant broken: %+v (len %d)", p, s, len(content))
			}
		}
	}
}

// The auditor's repro: an unterminated triple quote at EOF after a model
// string used to underflow the span interior and panic nearbyStrings.
// The full pipeline must survive every hostile file.
func TestAnalyzeSurvivesHostileFiles(t *testing.T) {
	r := analyzeTemp(t, map[string]string{
		"panic.py":  "m = \"gpt-4o\"\nx = \"\"\"a\n",
		"quotes.py": "m = \"gpt-4o-mini\"\ny = '''ab",
		"tick.ts":   "const m = \"gpt-5.1\";\nconst t = `",
		"open.js":   "const m = \"gpt-4o\";\nconst s = \"abc",
	})
	if len(r.Sites) == 0 {
		t.Error("hostile files still carry model refs; want sites, got none")
	}
}

// A string cut off by newline or EOF has no closing quote to exclude:
// the final interior byte must still be masked.
func TestMaskUnterminatedStringMasksFinalByte(t *testing.T) {
	m := maskFile("a.ts", `x = "abc`)
	if strings.Contains(m.all, "c") {
		t.Errorf("all = %q, want the unterminated interior fully blanked", m.all)
	}
	m2 := maskFile("a.ts", "y = \"abc\nz = 1\n")
	if strings.Contains(m2.all, "c") {
		t.Errorf("all = %q, want the newline-terminated interior fully blanked", m2.all)
	}
	if !strings.Contains(m2.all, "z = 1") {
		t.Errorf("all = %q, want the next line untouched", m2.all)
	}
}

// In Go raw strings a backslash is a literal byte, never an escape; a
// raw string ending in a backslash must not swallow its closing backtick
// and mask the rest of the file.
func TestGoRawStringBackslashIsLiteral(t *testing.T) {
	content := "package x\n\nvar dir = `C:\\some\\dir\\`\nvar model = \"claude-sonnet-5\"\n"
	m := maskFile("x.go", content)
	if !strings.Contains(m.all, "var model") {
		t.Errorf("all = %q, want the raw string closed at its backtick", m.all)
	}
	// JS template literals keep escape handling: an escaped backtick
	// stays inside the string.
	js := "const s = `a\\`b`;\nconst model = \"gpt-4o\";\n"
	mj := maskFile("x.ts", js)
	if strings.Contains(mj.all, "b`") && !strings.Contains(mj.all, "const model") {
		t.Errorf("all = %q, want the escaped backtick honored in JS", mj.all)
	}
	if idx := strings.Index(mj.all, "b"); idx >= 0 && js[idx] == 'b' && idx < strings.Index(js, "`;") {
		t.Errorf("all = %q, want b inside the template literal blanked", mj.all)
	}
}
