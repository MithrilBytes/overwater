package cli

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// Metamorphic properties: assertions about how the verdict must respond
// when the input changes in a known way, rather than about what the
// verdict is.
//
// The difference matters. A golden file and the fixture that produced it
// get written together from one reading of how the scanner should behave,
// so they agree with each other whether or not that reading was right.
// "Adding a comment must not move a finding" is true regardless, and it
// keeps being true after someone regenerates the goldens.
//
// Several of these guard bugs this repository has already had once:
// findings that depended on argument order, a config that leaked between
// roots, and CRLF changing a verdict.

// key identifies a finding independently of where it sits in the file, so
// a transformation that only moves code can be told apart from one that
// changes the verdict.
func key(f jsonFinding) string {
	return fmt.Sprintf("%s|%s|%s|%s|%d", f.Rule, f.File, f.Model, f.Archetype, f.MonthlyUSD)
}

func keys(report jsonReport) []string {
	out := make([]string, 0, len(report.Findings))
	for _, f := range report.Findings {
		out = append(out, key(f))
	}
	sort.Strings(out)
	return out
}

func assertSameFindings(t *testing.T, what string, before, after jsonReport) {
	t.Helper()
	got, want := keys(after), keys(before)
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("%s changed the verdict\nbefore (%d):\n  %s\nafter (%d):\n  %s",
			what, len(want), strings.Join(want, "\n  "), len(got), strings.Join(got, "\n  "))
	}
}

// tree writes files under a fresh temp dir and returns it.
func tree(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	writeInto(t, dir, files)
	return dir
}

func writeInto(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for name, content := range files {
		full := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// A call site that reliably produces findings: a frontier model on a
// classification task, no token cap, no caching.
const classifierTS = `import Anthropic from "@anthropic-ai/sdk";

const client = new Anthropic();

export async function classifyTicket(text: string) {
  return client.messages.create({
    model: "claude-opus-5",
    temperature: 0,
    system: "Classify the ticket into exactly one of: billing, bug, feature.",
    messages: [{ role: "user", content: text }],
  });
}
`

const summarizerPY = `from anthropic import Anthropic

client = Anthropic()

def summarize(doc):
    return client.messages.create(
        model="claude-opus-5",
        max_tokens=100000,
        system="Summarize the document in three sentences.",
        messages=[{"role": "user", "content": doc}],
    )
`

func baseTree(t *testing.T) string {
	return tree(t, map[string]string{
		"package.json":     `{"dependencies":{"@anthropic-ai/sdk":"^0.30.0"}}`,
		"src/classify.ts":  classifierTS,
		"src/summarize.py": summarizerPY,
	})
}

// The fixture has to actually produce findings or every property below
// passes by comparing two empty lists.
func TestMetamorphicFixtureProducesFindings(t *testing.T) {
	_, report, _ := scanJSON(t, baseTree(t))
	if len(report.Findings) < 2 {
		t.Fatalf("base tree produced %d findings, want at least 2; the "+
			"properties in this file would be vacuous", len(report.Findings))
	}
}

// Two runs over one tree must agree. Covers the whole pipeline, where the
// scan level determinism test covers only Analyze.
func TestPropertyPipelineIsDeterministic(t *testing.T) {
	dir := baseTree(t)
	_, first, _ := scanJSON(t, dir)
	for i := 0; i < 5; i++ {
		_, again, _ := scanJSON(t, dir)
		assertSameFindings(t, fmt.Sprintf("run %d", i+1), first, again)
	}
}

// Where the repository sits on disk is not a property of the code. This
// is the shape of bug that makes a scan pass locally and fail in CI.
func TestPropertyVerdictSurvivesRelocation(t *testing.T) {
	files := map[string]string{
		"package.json":     `{"dependencies":{"@anthropic-ai/sdk":"^0.30.0"}}`,
		"src/classify.ts":  classifierTS,
		"src/summarize.py": summarizerPY,
	}
	here := tree(t, files)
	elsewhere := filepath.Join(t.TempDir(), "nested", "deeper", "repo")
	if err := os.MkdirAll(elsewhere, 0o755); err != nil {
		t.Fatal(err)
	}
	writeInto(t, elsewhere, files)
	_, hereReport, _ := scanJSON(t, here)
	_, thereReport, _ := scanJSON(t, elsewhere)
	assertSameFindings(t, "relocating the repository", hereReport, thereReport)
}

// Comments and prose are masked before signals are read. Adding either
// must not create, remove, or reclassify a finding. Without this the
// masking can regress into "comments sometimes count" and only a
// hand-written case would notice.
func TestPropertyCommentsDoNotChangeTheVerdict(t *testing.T) {
	_, before, _ := scanJSON(t, baseTree(t))

	commented := strings.Replace(classifierTS,
		"export async function classifyTicket",
		`// TODO: max_tokens: 4000 and temperature: 0.9 belong here one day.
// The model should probably be "claude-haiku-4-5" for this.
/* Translate the ticket, then summarize it, then reply to the customer. */
export async function classifyTicket`, 1)

	_, after, _ := scanJSON(t, tree(t, map[string]string{
		"package.json":     `{"dependencies":{"@anthropic-ai/sdk":"^0.30.0"}}`,
		"src/classify.ts":  commented,
		"src/summarize.py": summarizerPY,
	}))
	assertSameFindings(t, "adding comments that mention models and caps", before, after)
}

// Reformatting moves line numbers and must move nothing else.
func TestPropertyBlankLinesMoveOnlyTheLineNumber(t *testing.T) {
	dir := baseTree(t)
	_, before, _ := scanJSON(t, dir)

	padded := "\n\n\n" + strings.ReplaceAll(classifierTS, "  return client", "\n  return client")
	_, after, _ := scanJSON(t, tree(t, map[string]string{
		"package.json":     `{"dependencies":{"@anthropic-ai/sdk":"^0.30.0"}}`,
		"src/classify.ts":  padded,
		"src/summarize.py": summarizerPY,
	}))

	// Same rules, models and prices; the classify line number is allowed
	// to move and is expected to.
	sameShape := func(r jsonReport) []string {
		out := []string{}
		for _, f := range r.Findings {
			out = append(out, fmt.Sprintf("%s|%s|%s|%d", f.Rule, f.File, f.Model, f.MonthlyUSD))
		}
		sort.Strings(out)
		return out
	}
	if strings.Join(sameShape(before), "\n") != strings.Join(sameShape(after), "\n") {
		t.Errorf("padding with blank lines changed the verdict\nbefore: %v\nafter:  %v",
			sameShape(before), sameShape(after))
	}
	if len(before.Findings) != len(after.Findings) {
		t.Fatalf("finding count changed: %d then %d", len(before.Findings), len(after.Findings))
	}
}

// A new file elsewhere in the tree must not disturb findings that were
// already there. Fan-in and import resolution both reach across files, so
// locality is a real property rather than an obvious one.
func TestPropertyUnrelatedFileIsLocal(t *testing.T) {
	_, before, _ := scanJSON(t, baseTree(t))

	files := map[string]string{
		"package.json":     `{"dependencies":{"@anthropic-ai/sdk":"^0.30.0"}}`,
		"src/classify.ts":  classifierTS,
		"src/summarize.py": summarizerPY,
		"src/util.ts":      "export function slug(s: string) {\n  return s.toLowerCase();\n}\n",
		"docs/notes.md":    "# Notes\n\nWe use claude-opus-5 for everything.\n",
	}
	_, after, _ := scanJSON(t, tree(t, files))
	assertSameFindings(t, "adding an unrelated file", before, after)
}

// Scanning one file must report exactly what a whole repository scan
// reports for that file. The single file path resolves imports and
// wrapper defaults from the containing directory, so the two can drift.
func TestPropertySingleFileAgreesWithRepoScan(t *testing.T) {
	dir := baseTree(t)
	_, whole, _ := scanJSON(t, dir)

	var want []string
	for _, f := range whole.Findings {
		if f.File == "src/classify.ts" || strings.HasSuffix(f.File, "classify.ts") {
			want = append(want, fmt.Sprintf("%s|%s|%d", f.Rule, f.Model, f.MonthlyUSD))
		}
	}
	sort.Strings(want)
	if len(want) == 0 {
		t.Skip("no finding in classify.ts to compare")
	}

	_, single, _ := scanJSON(t, filepath.Join(dir, "src", "classify.ts"))
	var got []string
	for _, f := range single.Findings {
		got = append(got, fmt.Sprintf("%s|%s|%d", f.Rule, f.Model, f.MonthlyUSD))
	}
	sort.Strings(got)
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("single file scan disagrees with the repo scan\nrepo:   %v\nsingle: %v", want, got)
	}
}

// Findings must not depend on the order the roots were named. This
// repository has shipped a bug where a per repo config leaked across
// roots and made the exit code depend on argument order.
func TestPropertyRootOrderDoesNotMatter(t *testing.T) {
	a := tree(t, map[string]string{
		"package.json":    `{"dependencies":{"@anthropic-ai/sdk":"^0.30.0"}}`,
		"src/classify.ts": classifierTS,
	})
	b := tree(t, map[string]string{
		"requirements.txt": "anthropic>=0.40\n",
		"src/summarize.py": summarizerPY,
	})

	_, forward, _ := scanJSON(t, a, b)
	_, backward, _ := scanJSON(t, b, a)
	assertSameFindings(t, "reversing the root order", forward, backward)
}

// An ignore pragma must remove its own finding and leave every other one
// standing.
func TestPropertyIgnorePragmaIsLocal(t *testing.T) {
	_, before, _ := scanJSON(t, baseTree(t))

	ignored := strings.Replace(classifierTS, "  return client.messages.create({",
		"  // overwater:ignore\n  return client.messages.create({", 1)
	_, after, _ := scanJSON(t, tree(t, map[string]string{
		"package.json":     `{"dependencies":{"@anthropic-ai/sdk":"^0.30.0"}}`,
		"src/classify.ts":  ignored,
		"src/summarize.py": summarizerPY,
	}))

	if len(after.Findings) >= len(before.Findings) {
		t.Fatalf("the ignore pragma removed nothing: %d findings then %d",
			len(before.Findings), len(after.Findings))
	}
	for _, f := range after.Findings {
		if strings.HasSuffix(f.File, "classify.ts") {
			t.Errorf("classify.ts still reports %s despite the ignore pragma", f.Rule)
		}
	}
	// Everything outside the ignored file must be untouched.
	survivors := func(r jsonReport) []string {
		out := []string{}
		for _, f := range r.Findings {
			if !strings.HasSuffix(f.File, "classify.ts") {
				out = append(out, key(f))
			}
		}
		sort.Strings(out)
		return out
	}
	if strings.Join(survivors(before), "\n") != strings.Join(survivors(after), "\n") {
		t.Errorf("the pragma disturbed findings in other files\nbefore: %v\nafter:  %v",
			survivors(before), survivors(after))
	}
}

// Copying a call site into a second file must add findings and remove
// none. A scanner that deduplicated too eagerly would lose the copy.
func TestPropertyDuplicatingACallSiteIsAdditive(t *testing.T) {
	_, before, _ := scanJSON(t, baseTree(t))

	_, after, _ := scanJSON(t, tree(t, map[string]string{
		"package.json":     `{"dependencies":{"@anthropic-ai/sdk":"^0.30.0"}}`,
		"src/classify.ts":  classifierTS,
		"src/classify2.ts": classifierTS,
		"src/summarize.py": summarizerPY,
	}))

	if len(after.Findings) <= len(before.Findings) {
		t.Errorf("duplicating a call site did not add findings: %d then %d",
			len(before.Findings), len(after.Findings))
	}
	had := map[string]bool{}
	for _, f := range after.Findings {
		had[key(f)] = true
	}
	for _, f := range before.Findings {
		if !had[key(f)] {
			t.Errorf("duplication removed an existing finding: %s", key(f))
		}
	}
}

// A baseline recorded from a tree must green that same tree. If it did
// not, the ratchet would fail a build that changed nothing.
func TestPropertyBaselineOfATreeGreensThatTree(t *testing.T) {
	dir := baseTree(t)
	bl := filepath.Join(t.TempDir(), "baseline.json")

	var out, errOut bytes.Buffer
	if code := Run([]string{"scan", "--baseline", bl, "--update-baseline", dir}, &out, &errOut); code == 2 {
		t.Fatalf("recording the baseline exited 2: %s", errOut.String())
	}
	out.Reset()
	errOut.Reset()
	code := Run([]string{"scan", "--baseline", bl, "--fail-on", "new", dir}, &out, &errOut)
	if code != 0 {
		t.Errorf("a tree scanned against its own baseline exited %d, want 0\n%s\n%s",
			code, out.String(), errOut.String())
	}
}

// A file that moves is the same file: git mv must leave every finding's
// rule, model and price alone and move nothing but the path.
//
// The verdict half of that holds. The ratchet half does not. A
// fingerprint hashes the repo relative path, so every finding in a moved
// file reads as new against a baseline recorded before the move, and a
// rename that touched no call site fails the build. Rename stable
// fingerprints is an open roadmap item; the second half of this test
// pins what the guard does today, so landing it turns this red instead
// of going unnoticed.
func TestPropertyRenameMovesOnlyThePath(t *testing.T) {
	gitOrSkip(t)
	// legacy.js never moves, so the ratchet half can tell churn caused by
	// the rename from churn across the whole baseline.
	dir := tree(t, map[string]string{
		"package.json":     `{"dependencies":{"@anthropic-ai/sdk":"^0.30.0"}}`,
		"src/classify.ts":  classifierTS,
		"src/summarize.py": summarizerPY,
		"src/legacy.js":    legacyCall,
	})
	initRepo(t, dir)
	_, before, _ := scanJSON(t, dir)

	const from, to = "src/classify.ts", "src/tickets/classify.ts"
	var want []string
	moved := 0
	for _, f := range before.Findings {
		if f.File == from {
			f.File = to
			moved++
		}
		want = append(want, key(f))
	}
	sort.Strings(want)
	if moved < 1 || len(want) == moved {
		t.Fatalf("fixture: %d of %d findings sit in %s; the property needs findings on both sides of the move",
			moved, len(want), from)
	}

	bl := filepath.Join(t.TempDir(), "baseline.json")
	var out, errOut bytes.Buffer
	if code := Run([]string{"scan", "--baseline", bl, "--update-baseline", dir}, &out, &errOut); code == ExitError {
		t.Fatalf("recording the baseline exited 2: %s", errOut.String())
	}

	// Somewhere neutral: under tests/ or named .spec the sites would be
	// suppressed on purpose, which is a different property.
	if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, filepath.FromSlash(to))), 0o755); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "mv", from, to)
	gitRun(t, dir, "commit", "-q", "-m", "move the classifier")

	_, after, _ := scanJSON(t, dir)
	// Fatal, not an error: with the verdict itself moved there is nothing
	// left for the ratchet half below to read.
	if got := keys(after); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("renaming %s changed more than the path\nwant (%d):\n  %s\ngot (%d):\n  %s",
			from, len(want), strings.Join(want, "\n  "), len(got), strings.Join(got, "\n  "))
	}

	// The ratchet half. A move retired every baselined finding in the
	// file and reintroduced an identical set as new, so a commit that
	// changed no behaviour failed the build. The baseline now records
	// each finding's call site hash beside its fingerprint and matches
	// on that when the path no longer lines up.
	out.Reset()
	errOut.Reset()
	code := Run([]string{"scan", "--baseline", bl, "--fail-on", "new", dir}, &out, &errOut)
	if code != ExitClean {
		t.Fatalf("the ratchet exited %d after a pure rename, not %d\n%s\n%s",
			code, ExitClean, out.String(), errOut.String())
	}
	if strings.Contains(errOut.String(), "new") {
		t.Errorf("stderr = %q, want nothing reported as new", errOut.String())
	}
}

// callersTS reaches the classifier from three places. Fan in prices that
// call site at three times the volume, and this file never changes, so
// an incremental scan can only agree with a full one by reading it.
const callersTS = `import { classifyTicket } from "./classify";

export async function onWebhook(text: string) {
  return classifyTicket(text);
}

export async function onEmail(text: string) {
  return classifyTicket(text);
}

export async function onChat(text: string) {
  return classifyTicket(text);
}
`

// routerTS is a second call, deliberately not a copy of the classifier's:
// duplicate-call-sites counts the sites a scan emitted, so a copy whose
// twin sits in an unscanned file is the one thing the two scans do not
// agree on.
const routerTS = `import Anthropic from "@anthropic-ai/sdk";

const client = new Anthropic();

export async function routeMessage(text: string) {
  return client.messages.create({
    model: "claude-opus-5",
    temperature: 0,
    system: "Route the message to exactly one of: sales, support, abuse.",
    messages: [{ role: "user", content: text }],
  });
}
`

// A scan restricted to what git says changed must report exactly what a
// whole repository scan reports for those same files. That is the whole
// claim --incremental makes in CI, and the incremental tests cover the
// coverage note, the ratchet and quoted paths without ever asserting it.
// The restriction decides which files may produce sites, not which files
// inform them, so an unscanned caller still has to count.
func TestPropertyIncrementalAgreesWithFullScan(t *testing.T) {
	gitOrSkip(t)
	dir := tree(t, map[string]string{
		"package.json":     `{"dependencies":{"@anthropic-ai/sdk":"^0.30.0"}}`,
		"src/classify.ts":  classifierTS,
		"src/handlers.ts":  callersTS,
		"src/summarize.py": summarizerPY,
	})
	initRepo(t, dir)

	bl := filepath.Join(t.TempDir(), "baseline.json")
	var out, errOut bytes.Buffer
	if code := Run([]string{"scan", "--baseline", bl, "--update-baseline", dir}, &out, &errOut); code == ExitError {
		t.Fatalf("recording the baseline exited 2: %s", errOut.String())
	}

	// One tracked file edited and one file added: git names the first
	// through diff and the second through ls-files.
	changed := map[string]bool{"src/classify.ts": true, "src/route.ts": true}
	writeInto(t, dir, map[string]string{
		"src/classify.ts": strings.Replace(classifierTS, "billing, bug, feature", "billing, bug, feature, other", 1),
		"src/route.ts":    routerTS,
	})

	_, full, _ := scanJSON(t, dir)
	var want []string
	perFile := map[string]int{}
	for _, f := range full.Findings {
		if changed[f.File] {
			want = append(want, key(f))
			perFile[f.File]++
		}
	}
	sort.Strings(want)
	for name := range changed {
		if perFile[name] == 0 {
			t.Fatalf("fixture: the full scan reports nothing in %s, so the comparison is vacuous", name)
		}
	}

	_, incremental, stderr := scanJSON(t, "--baseline", bl, "--incremental", dir)
	if !strings.Contains(stderr, "incremental: scanned 2 of 2 candidate files") {
		t.Fatalf("stderr = %q, want both changed files scanned; the comparison is against the wrong set", stderr)
	}
	if got := keys(incremental); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("--incremental disagrees with the full scan over the files it scanned\nfull (%d):\n  %s\nincremental (%d):\n  %s",
			len(want), strings.Join(want, "\n  "), len(got), strings.Join(got, "\n  "))
	}
}

// The failure policy is the only thing that decides exit 1. Whatever the
// findings, "none" must never fail the build, and the policy must not
// change what was found.
func TestPropertyFailOnNoneNeverFails(t *testing.T) {
	dir := baseTree(t)
	var out, errOut bytes.Buffer
	code := Run([]string{"scan", "--fail-on", "none", dir}, &out, &errOut)
	if code != 0 {
		t.Errorf("--fail-on none exited %d, want 0\n%s", code, errOut.String())
	}

	_, strict, _ := scanJSON(t, "--fail-on", "any", dir)
	_, lenient, _ := scanJSON(t, "--fail-on", "none", dir)
	assertSameFindings(t, "changing the failure policy", strict, lenient)
}
