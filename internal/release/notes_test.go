package release

import (
	"strings"
	"testing"
)

func TestNotes(t *testing.T) {
	cases := []struct {
		name     string
		subjects []string
		prev     string
		tag      string
		want     string
	}{
		{
			name: "grouped in heading order regardless of log order",
			subjects: []string{
				"chore: pin the action to the v2.1.0 release binaries",
				"fix(scan): do not score a task phrase the prompt negates",
				"feat(cli): accept a file path as a scan root",
				"perf: gate CI on how analysis time scales with input",
				"refactor(rules): lift duplicate counting out of Evaluate",
				"docs: describe fan-in pricing",
				"test(scan): floor accuracy per split",
				"feat: add the fleet command",
			},
			prev: "v2.1.0",
			tag:  "v2.1.1",
			want: `### Features

- cli: accept a file path as a scan root
- add the fleet command

### Fixes

- scan: do not score a task phrase the prompt negates

### Performance

- gate CI on how analysis time scales with input

### Refactors

- rules: lift duplicate counting out of Evaluate

### Documentation

- describe fan-in pricing

### Tests

- scan: floor accuracy per split

### Chores

- pin the action to the v2.1.0 release binaries

Static overwater binaries. Verify downloads against SHA256SUMS.

[All commits since v2.1.0](https://github.com/MithrilBytes/overwater/compare/v2.1.0...v2.1.1)
`,
		},
		{
			name: "unknown and unparseable types keep their whole subject",
			subjects: []string{
				"build(deps): bump nothing",
				"Merge branch 'main' into topic",
				"revert: feat(cli): add explain",
				"docs:",
				"",
				"   ",
			},
			prev: "v2.1.0",
			tag:  "v2.1.1",
			want: `### Other

- build(deps): bump nothing
- Merge branch 'main' into topic
- revert: feat(cli): add explain
- docs:

Static overwater binaries. Verify downloads against SHA256SUMS.

[All commits since v2.1.0](https://github.com/MithrilBytes/overwater/compare/v2.1.0...v2.1.1)
`,
		},
		{
			name:     "an empty log still says what shipped and links the range",
			subjects: nil,
			prev:     "v2.1.0",
			tag:      "v2.1.1",
			want: `No commits since v2.1.0.

Static overwater binaries. Verify downloads against SHA256SUMS.

[All commits since v2.1.0](https://github.com/MithrilBytes/overwater/compare/v2.1.0...v2.1.1)
`,
		},
		{
			name:     "a first release with an empty log has no earlier tag to name",
			subjects: nil,
			prev:     "",
			tag:      "v1.0.0",
			want: `No commits.

Static overwater binaries. Verify downloads against SHA256SUMS.

[All commits](https://github.com/MithrilBytes/overwater/commits/v1.0.0)
`,
		},
		{
			name:     "the first release lists commits instead of comparing",
			subjects: []string{"feat: scan a repo for LLM call sites"},
			prev:     "",
			tag:      "v1.0.0",
			want: `### Features

- scan a repo for LLM call sites

Static overwater binaries. Verify downloads against SHA256SUMS.

[All commits](https://github.com/MithrilBytes/overwater/commits/v1.0.0)
`,
		},
		{
			name: "markdown in a subject reaches the reader as text",
			subjects: []string{
				"feat(cli): add explain <rule-id>",
				"fix: keep *emphasis* and _underscores_ literal",
				"docs: keep [links](x) unclickable",
				"chore: escape a \\ and a # too",
				"nope [see](http://example.com)",
			},
			prev: "v2.1.0",
			tag:  "v2.1.1",
			want: `### Features

- cli: add explain \<rule-id\>

### Fixes

- keep \*emphasis\* and \_underscores\_ literal

### Documentation

- keep \[links\](x) unclickable

### Chores

- escape a \\ and a \# too

### Other

- nope \[see\](http://example.com)

Static overwater binaries. Verify downloads against SHA256SUMS.

[All commits since v2.1.0](https://github.com/MithrilBytes/overwater/compare/v2.1.0...v2.1.1)
`,
		},
		{
			name:     "a breaking marker groups by type and survives into the item",
			subjects: []string{"feat(cli)!: drop the --legacy flag", "fix!: change the exit code"},
			prev:     "v2.1.0",
			tag:      "v3.0.0",
			want: `### Features

- breaking: cli: drop the --legacy flag

### Fixes

- breaking: change the exit code

Static overwater binaries. Verify downloads against SHA256SUMS.

[All commits since v2.1.0](https://github.com/MithrilBytes/overwater/compare/v2.1.0...v3.0.0)
`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Notes(tc.subjects, tc.prev, tc.tag, "MithrilBytes/overwater")
			if got != tc.want {
				t.Errorf("notes mismatch\n--- got ---\n%s\n--- want ---\n%s", got, tc.want)
			}
		})
	}
}

// A backtick in a subject would open a code span and swallow the text
// after it, so it is escaped like the rest of the inline markdown.
func TestBacktickIsEscaped(t *testing.T) {
	got := Notes([]string{"docs: quote `overwater scan` plainly"}, "v1", "v2", "r/r")
	want := "- quote \\`overwater scan\\` plainly\n"
	if !strings.Contains(got, want) {
		t.Errorf("got:\n%s\nwant a line: %s", got, want)
	}
}

// Every heading a subject can land under has to be one the renderer emits,
// or a type would be grouped into a section that never prints.
func TestEveryHeadingIsEmitted(t *testing.T) {
	for _, g := range groups {
		out := Notes([]string{g.typ + ": subject"}, "v1", "v2", "r/r")
		if !strings.Contains(out, "### "+g.heading+"\n\n- subject\n") {
			t.Errorf("type %q did not render under %q:\n%s", g.typ, g.heading, out)
		}
	}
}

// The two lanes advance independently: updates count from the last
// human release, twins own the patch lane so go install can resolve
// something.
func TestNextTags(t *testing.T) {
	cases := []struct {
		name         string
		tags         []string
		update, twin string
	}{
		{"first update", []string{"v2.1.0", "v2.2.0", "v2.2.1"}, "v2.2.1.1", "v2.2.2"},
		{"second update", []string{"v2.2.1", "v2.2.1.1", "v2.2.2"}, "v2.2.1.2", "v2.2.3"},
		{"third update", []string{"v2.2.1", "v2.2.1.2", "v2.2.3"}, "v2.2.1.3", "v2.2.4"},
		{"ninth to tenth", []string{"v2.2.1", "v2.2.1.9", "v2.2.10"}, "v2.2.1.10", "v2.2.11"},
		{"minor bump restarts", []string{"v2.2.1", "v2.2.1.3", "v2.2.4", "v2.3.0"}, "v2.3.0.1", "v2.3.1"},
		{"major bump restarts", []string{"v2.2.1.3", "v2.2.4", "v3.0.0"}, "v3.0.0.1", "v3.0.1"},
		{"unsorted input", []string{"v2.2.2", "v2.2.1.1", "v2.2.1"}, "v2.2.1.2", "v2.2.3"},
		{"junk ignored", []string{"latest", "v2.2.1", "not-a-tag", "v2"}, "v2.2.1.1", "v2.2.2"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			update, twin, err := NextTags(tt.tags)
			if err != nil {
				t.Fatalf("NextTags(%v) errored: %v", tt.tags, err)
			}
			if update != tt.update || twin != tt.twin {
				t.Errorf("NextTags(%v) = %q, %q; want %q, %q", tt.tags, update, twin, tt.update, tt.twin)
			}
		})
	}
}

func TestNextTagsNeedsAFixTag(t *testing.T) {
	for _, tags := range [][]string{nil, {}, {"latest"}, {"v2.2.1.1"}} {
		if u, tw, err := NextTags(tags); err == nil {
			t.Errorf("NextTags(%v) = %q, %q; want an error", tags, u, tw)
		}
	}
}

// Ten consecutive updates stay ordered by git's own version sort, which
// is what picks the base on the next run.
func TestNextTagsStayOrdered(t *testing.T) {
	tags := []string{"v2.2.1"}
	prevUpdate := ""
	for i := 0; i < 10; i++ {
		update, twin, err := NextTags(tags)
		if err != nil {
			t.Fatal(err)
		}
		if update == prevUpdate {
			t.Fatalf("update %q repeated on round %d", update, i)
		}
		prevUpdate = update
		tags = append(tags, update, twin)
	}
	if prevUpdate != "v2.2.1.10" {
		t.Errorf("after ten updates the last is %q, want v2.2.1.10", prevUpdate)
	}
}
