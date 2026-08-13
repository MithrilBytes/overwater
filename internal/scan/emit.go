package scan

import (
	"path"
	"strings"
)

// Which files may produce call sites.
//
// Every file is read: a prompt, a constant or a caller can live anywhere
// and layers 3 and 4 need the whole repository as context. Emitting a
// site is a narrower question, and the answer is no for two kinds of
// file that name models without ever calling one.
//
// Documentation and configuration describe calls. A README listing
// supported models, a front matter block recording which agent wrote the
// document, a YAML roster of what a user may pick from: none of these
// spends a token. Configuration that a program actually reads is still
// detected, by traceConfigModels, which ties the value to the code that
// reads it and reports the site there. A config file with no reader
// produces nothing, which is correct.
//
// Test files and fixtures name models in order to assert on them. They
// run in CI, not in production, and pricing them per month is a category
// error. They stay as context so a test that calls a wrapper still
// counts toward its fan in.
//
// This was measured rather than guessed. Against 128 real repositories,
// documentation and configuration accounted for the largest share of
// phantom sites: one configuration repository produced 4,964 of them and
// makes no LLM call at all.

// docExts never hold a call. Notebooks are deliberately absent: .ipynb
// is JSON, but it is JSON wrapped around real source.
var docExts = map[string]bool{
	".md": true, ".markdown": true, ".mdx": true,
	".rst": true, ".adoc": true, ".txt": true,
}

// configExts hold values a program may read. traceConfigModels covers
// the ones with a reader; layer 2 must not also report the raw line.
var configExts = map[string]bool{
	".yaml": true, ".yml": true, ".toml": true,
	".ini": true, ".properties": true, ".cfg": true,
}

// testDirs name a directory whose contents are tests wherever it sits in
// the tree.
var testDirs = map[string]bool{
	"test": true, "tests": true, "__tests__": true, "testdata": true,
	"spec": true, "specs": true, "__mocks__": true, "fixtures": true,
	"e2e": true, "cypress": true,
}

// emitsSites reports whether a file may produce call sites of its own.
// The path is slash separated and relative to the scan root, so a
// repository whose own root is a test corpus still scans normally.
func emitsSites(p string) bool {
	ext := strings.ToLower(path.Ext(p))
	if docExts[ext] || configExts[ext] {
		return false
	}
	base := path.Base(p)
	if strings.HasPrefix(base, ".env") {
		return false
	}
	return !isTestPath(p)
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

	// Go, Rust: foo_test.go. Python: test_foo.py and foo_test.py.
	if strings.HasSuffix(name, "_test") || strings.HasPrefix(name, "test_") {
		return true
	}
	// JavaScript and friends: foo.test.ts, foo.spec.tsx. The extension
	// is already stripped, so the marker is now the trailing segment.
	if strings.HasSuffix(name, ".test") || strings.HasSuffix(name, ".spec") {
		return true
	}
	// Ruby: foo_spec.rb. C#: FooTests.cs.
	if strings.HasSuffix(name, "_spec") || strings.HasSuffix(name, "tests") {
		return true
	}
	// Jest snapshots.
	return strings.HasSuffix(lower, ".snap")
}
