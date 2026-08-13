package scan

import (
	"path"
	"regexp"
	"strings"
)

// Which files may produce call sites, and which lines of them.
//
// Every file is read: a prompt, a constant or a caller can live anywhere
// and layers 3 and 4 need the whole repository as context. Emitting a
// site is a narrower question.
//
// Documentation never calls anything. A README listing supported models,
// or front matter recording which agent wrote the document, spends no
// tokens.
//
// Test files and fixtures name models in order to assert on them. They
// run in CI, not in production, and pricing them per month is a category
// error. They stay as context, so a test that calls a wrapper still
// counts toward its fan in.
//
// Configuration is the interesting case, because it is both. A model
// bound to a key is a real call site: the program reads it and the shape
// around it is readable, which is why config style files have their own
// regex fallback. A model sitting in a list is a roster of what the
// operator may choose, and nothing calls it. The two look similar and
// price very differently: one configuration repository of nothing but
// rosters produced 4,964 sites and makes no LLM call at all.
//
// All of this was measured against 128 real repositories, not guessed.

var docExts = map[string]bool{
	".md": true, ".markdown": true, ".mdx": true,
	".rst": true, ".adoc": true, ".txt": true,
}

// Notebooks are deliberately absent: .ipynb is JSON wrapped around real
// source, and .json is absent because a JSON config has no reader
// convention to key on.
var configExts = map[string]bool{
	".yaml": true, ".yml": true, ".toml": true,
	".ini": true, ".properties": true, ".cfg": true,
}

var testDirs = map[string]bool{
	"test": true, "tests": true, "__tests__": true, "testdata": true,
	"spec": true, "specs": true, "__mocks__": true, "fixtures": true,
	"e2e": true, "cypress": true,
}

// emitsSites reports whether a file may produce call sites at all. The
// path is slash separated and relative to the scan root, so a repository
// whose own root is a test corpus still scans normally.
func emitsSites(p string) bool {
	if docExts[strings.ToLower(path.Ext(p))] {
		return false
	}
	if strings.HasPrefix(path.Base(p), ".env") {
		return false // traceConfigModels reports these at their reader
	}
	return !isTestPath(p)
}

func isConfigPath(p string) bool {
	return configExts[strings.ToLower(path.Ext(p))]
}

// isTestPath reports whether p is a test, a spec, or fixture data, by
// the naming conventions of the languages overwater parses.
func isTestPath(p string) bool {
	dir, base := path.Split(p)
	for _, seg := range strings.Split(strings.Trim(dir, "/"), "/") {
		if testDirs[strings.ToLower(seg)] {
			return true
		}
	}
	lower := strings.ToLower(base)
	name := strings.TrimSuffix(lower, path.Ext(lower))

	// Go and Rust: foo_test.go. Python: test_foo.py and foo_test.py.
	if strings.HasSuffix(name, "_test") || strings.HasPrefix(name, "test_") {
		return true
	}
	// JavaScript and friends: foo.test.ts, foo.spec.tsx.
	if strings.HasSuffix(name, ".test") || strings.HasSuffix(name, ".spec") {
		return true
	}
	// Ruby: foo_spec.rb. C#: FooTests.cs.
	if strings.HasSuffix(name, "_spec") || strings.HasSuffix(name, "tests") {
		return true
	}
	return strings.HasSuffix(lower, ".snap")
}

// modelKeyish reports whether a key or section name implies that its value
// names a model. The same vocabulary traceConfigModels uses, lowercased
// because yaml and toml keys are not shouted the way env vars are.
func modelKeyish(name string) bool {
	n := strings.ToLower(name)
	for _, want := range []string{"model", "deployment", "engine", "llm"} {
		if strings.Contains(n, want) {
			return true
		}
	}
	return false
}

var (
	reSection = regexp.MustCompile(`^\s*\[([^\]]+)\]\s*$`)
	reKeyVal  = regexp.MustCompile(`^(\s*)([\w.$-]+)\s*[:=]\s*(.*)$`)
)

// configBindings returns the 1-based lines of a config file on which a
// model is bound to a key, rather than listed as one option among many.
//
// A binding is a scalar value whose key, or whose enclosing section or
// parent key, names a model: `model: gpt-5-mini`, or `name = mistral`
// under an `[model]` section. A bare sequence item has no key and is
// never a binding, which is what a roster is made of.
func configBindings(content string) map[int]bool {
	allowed := map[int]bool{}
	section := ""
	// Parent keys by indent, for the nesting yaml uses instead of
	// sections. Each entry is a key whose value was a block.
	type parent struct {
		indent int
		key    string
	}
	var stack []parent

	for i, line := range strings.Split(content, "\n") {
		if m := reSection.FindStringSubmatch(line); m != nil {
			section = m[1]
			stack = stack[:0]
			continue
		}
		m := reKeyVal.FindStringSubmatch(line)
		if m == nil {
			continue // a sequence item, a comment, or blank
		}
		indent, key, value := len(m[1]), m[2], strings.TrimSpace(m[3])
		for len(stack) > 0 && stack[len(stack)-1].indent >= indent {
			stack = stack[:len(stack)-1]
		}
		// An empty value opens a block; the key becomes a parent. So
		// does a value that is only a comment.
		if value == "" || strings.HasPrefix(value, "#") {
			stack = append(stack, parent{indent: indent, key: key})
			continue
		}
		// A value that opens a collection is a roster, not a binding,
		// however model-ish its key: `models: [a, b]`.
		if strings.HasPrefix(value, "[") || strings.HasPrefix(value, "{") {
			continue
		}
		if modelKeyish(key) || modelKeyish(section) {
			allowed[i+1] = true
			continue
		}
		for _, p := range stack {
			if modelKeyish(p.key) {
				allowed[i+1] = true
				break
			}
		}
	}
	return allowed
}

// keepConfigBindings drops sites that sit on a config line which does
// not bind a model to a key.
func keepConfigBindings(content string, sites []Site) []Site {
	allowed := configBindings(content)
	kept := sites[:0]
	for _, s := range sites {
		if allowed[s.Line] {
			kept = append(kept, s)
		}
	}
	return kept
}
