// Package release renders a tag's GitHub release notes from its commit
// log. The release workflow pipes git log subjects through cmd/ in this
// package; packaging_test.go pins the workflow and Dockerfile that use it.
package release

import (
	"fmt"
	"regexp"
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

var (
	reFixTag = regexp.MustCompile(`^v?([0-9]+)\.([0-9]+)\.([0-9]+)$`)
)

// NextTag returns the tag to push for a price change: the patch after
// the highest release.
//
// This used to be two tags. A price change carried a fourth component,
// v2.2.1.1, on the theory that a catalog refresh is a different kind of
// event from a code change and should read as one. It is, but nobody
// needed to be told: the tag is not where that belongs, the release
// notes are. Four component tags are not semver, so every one of them
// needed a twin pushed at the same commit for go install to resolve,
// which meant two tags, two lanes advancing independently, and a rule
// about which lane owns the patch number. No four component tag was
// ever pushed before the scheme came out.
func NextTag(tags []string, bump string) (string, error) {
	var best []int
	for _, t := range tags {
		if m := reFixTag.FindStringSubmatch(strings.TrimSpace(t)); m != nil {
			if v := ints(m[1:]); higher(v, best) {
				best = v
			}
		}
	}
	if best == nil {
		return "", fmt.Errorf("no vMAJOR.MINOR.PATCH tag to bump from")
	}
	switch bump {
	case "", "patch":
		return fmt.Sprintf("v%d.%d.%d", best[0], best[1], best[2]+1), nil
	case "minor":
		return fmt.Sprintf("v%d.%d.0", best[0], best[1]+1), nil
	case "major":
		return fmt.Sprintf("v%d.0.0", best[0]+1), nil
	}
	return "", fmt.Errorf("bump %q is not patch, minor or major", bump)
}

// BumpFor reads the commit subjects since the last tag and says what
// kind of release they add up to, by the prefixes the notes already
// group on: a feat is a minor, a fix or perf is a patch, and anything
// else on its own is not a release. A breaking marker is a major, which
// the automation refuses to cut, because a major moves the floating tag
// every README example names and that wants a person. The strongest
// prefix wins.
func BumpFor(subjects []string) string {
	bump := ""
	for _, subject := range subjects {
		prefix, _, ok := strings.Cut(subject, ":")
		if !ok {
			continue
		}
		prefix = strings.TrimSpace(prefix)
		if strings.HasSuffix(prefix, "!") {
			return "major"
		}
		if i := strings.IndexByte(prefix, '('); i >= 0 {
			prefix = prefix[:i]
		}
		switch strings.ToLower(prefix) {
		case "feat":
			bump = "minor"
		case "fix", "perf":
			if bump == "" {
				bump = "patch"
			}
		}
	}
	return bump
}

func ints(parts []string) []int {
	out := make([]int, len(parts))
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			return nil
		}
		out[i] = n
	}
	return out
}

// higher compares version components left to right.
func higher(a, b []int) bool {
	if b == nil {
		return true
	}
	for i := range a {
		if i >= len(b) || a[i] != b[i] {
			return i >= len(b) || a[i] > b[i]
		}
	}
	return false
}
