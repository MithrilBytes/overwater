// Package release renders a tag's GitHub release notes from its commit
// log. The release workflow pipes git log subjects through cmd/ in this
// package; packaging_test.go pins the workflow and Dockerfile that use it.
package release

import (
	"fmt"
	"strconv"
	"strings"
)

// groups maps a Conventional Commit type to its heading, in the order the
// headings are emitted.
var groups = []struct{ typ, heading string }{
	{"feat", "Features"},
	{"fix", "Fixes"},
	{"perf", "Performance"},
	{"refactor", "Refactors"},
	{"docs", "Documentation"},
	{"test", "Tests"},
	{"chore", "Chores"},
}

// otherHeading takes subjects with an unlisted type and subjects that are
// not Conventional Commits at all. Their full subject line is kept.
const otherHeading = "Other"

// verifyLine is the fixed footer every release carries.
const verifyLine = "Static overwater binaries. Verify downloads against SHA256SUMS."

// Notes renders the release notes for tag from the commit subjects since
// prevTag, grouped by Conventional Commit type. repo is the "owner/name"
// slug the trailing link points at. An empty prevTag means tag is the
// first release, so the link lists the tag's commits instead of a compare.
func Notes(subjects []string, prevTag, tag, repo string) string {
	items := map[string][]string{}
	for _, s := range subjects {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		heading, item := classify(s)
		items[heading] = append(items[heading], item)
	}

	var b strings.Builder
	headings := make([]string, 0, len(groups)+1)
	for _, g := range groups {
		headings = append(headings, g.heading)
	}
	headings = append(headings, otherHeading)
	for _, h := range headings {
		if len(items[h]) == 0 {
			continue
		}
		fmt.Fprintf(&b, "### %s\n\n", h)
		for _, item := range items[h] {
			fmt.Fprintf(&b, "- %s\n", item)
		}
		b.WriteString("\n")
	}
	if b.Len() == 0 {
		if prevTag == "" {
			b.WriteString("No commits.\n\n")
		} else {
			fmt.Fprintf(&b, "No commits since %s.\n\n", prevTag)
		}
	}

	fmt.Fprintf(&b, "%s\n\n", verifyLine)
	if prevTag == "" {
		fmt.Fprintf(&b, "[All commits](https://github.com/%s/commits/%s)\n", repo, tag)
	} else {
		fmt.Fprintf(&b, "[All commits since %s](https://github.com/%s/compare/%s...%s)\n", prevTag, repo, prevTag, tag)
	}
	return b.String()
}

// classify sorts one subject into a heading and renders its list item.
// A known type is dropped from the item, since the heading already says
// it, and the scope is kept as a prefix. Anything else keeps its whole
// subject line, so nothing in the log goes missing from the notes.
func classify(subject string) (heading, item string) {
	prefix, rest, ok := strings.Cut(subject, ":")
	if !ok {
		return otherHeading, escape(subject)
	}
	prefix, rest = strings.TrimSpace(prefix), strings.TrimSpace(rest)
	breaking := strings.HasSuffix(prefix, "!")
	prefix = strings.TrimSuffix(prefix, "!")
	typ, scope := prefix, ""
	if i := strings.IndexByte(prefix, '('); i > 0 && strings.HasSuffix(prefix, ")") {
		typ, scope = prefix[:i], prefix[i+1:len(prefix)-1]
	}
	if rest == "" {
		return otherHeading, escape(subject)
	}
	for _, g := range groups {
		if g.typ != typ {
			continue
		}
		item = escape(rest)
		if scope != "" {
			item = escape(scope) + ": " + item
		}
		if breaking {
			item = "breaking: " + item
		}
		return g.heading, item
	}
	return otherHeading, escape(subject)
}

// escape neutralizes the inline markdown a commit subject can carry, so
// "add explain <rule-id>" reaches the reader instead of being parsed as
// an HTML tag and dropped.
func escape(s string) string {
	const special = "\\`*_[]<>#"
	var b strings.Builder
	for _, r := range s {
		if r < 0x80 && strings.ContainsRune(special, r) {
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
}

// NextPatch returns the tag one patch above the given one, so a merged
// price change can ship without a human picking a number.
func NextPatch(tag string) (string, error) {
	trimmed := strings.TrimPrefix(tag, "v")
	parts := strings.Split(trimmed, ".")
	if len(parts) != 3 {
		return "", fmt.Errorf("tag %q is not vMAJOR.MINOR.PATCH", tag)
	}
	nums := make([]int, 3)
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return "", fmt.Errorf("tag %q is not vMAJOR.MINOR.PATCH", tag)
		}
		nums[i] = n
	}
	return fmt.Sprintf("v%d.%d.%d", nums[0], nums[1], nums[2]+1), nil
}
