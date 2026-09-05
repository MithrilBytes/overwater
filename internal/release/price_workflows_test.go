package release

import (
	"strings"
	"testing"
)

// price-watch commits what it computed and releases it with no person
// in the loop. It does not run until it fires for real, so the guards
// that keep that path from shipping garbage are pinned here.

// A run block is bash -e but not pipefail, so a pipeline reports its last
// stage's status: the catalog diff exiting 2 arrives as tee's or sort's 0
// and the steps below it commit against a catalog that was never written.
func TestPriceWatchStepsSetPipefail(t *testing.T) {
	steps := readWorkflow(t, "price-watch.yml").Jobs["diff"].Steps
	if len(steps) == 0 {
		t.Fatal("price-watch.yml has no diff job to check")
	}
	for _, s := range steps {
		if s.Run == "" {
			continue
		}
		if !strings.HasPrefix(strings.TrimSpace(s.Run), "set -euo pipefail") {
			t.Errorf("price-watch step %q does not open with set -euo pipefail", s.Name)
		}
	}
}

// The roster is copied over the committed one and nothing else in the
// repo reads it, so CI cannot catch a wiped one. An empty list is the
// diff having printed nothing, not upstream dropping every model.
func TestPriceWatchGuardsTheRosterCopy(t *testing.T) {
	var run string
	for _, s := range readWorkflow(t, "price-watch.yml").Jobs["diff"].Steps {
		if strings.Contains(s.Run, `cp "$RUNNER_TEMP/unlisted.txt" catalog/unlisted.txt`) {
			run = s.Run
		}
	}
	if run == "" {
		t.Fatal("no price-watch step commits the roster; this test guards that copy")
	}
	guard := strings.Index(run, `[ ! -s "$RUNNER_TEMP/unlisted.txt" ]`)
	if guard < 0 {
		t.Fatal("price-watch copies the roster over the committed one without checking it carries anything")
	}
	if guard > strings.Index(run, `cp "$RUNNER_TEMP/unlisted.txt"`) {
		t.Error("the empty roster guard runs after the copy it exists to prevent")
	}
}

// What ships on its own was tested moments before by the same job,
// because a push made with GITHUB_TOKEN starts no ci run to test it
// afterward. The order is the guard: apply, regold, test, then ship.
func TestPriceWatchTestsBeforeItShips(t *testing.T) {
	steps := readWorkflow(t, "price-watch.yml").Jobs["diff"].Steps
	pos := map[string]int{}
	for i, st := range steps {
		pos[st.Name] = i
	}
	apply, ok := pos["apply what passed the guards"]
	if !ok {
		t.Fatal("price-watch.yml has no apply step")
	}
	ship, ok := pos["ship on its own"]
	if !ok {
		t.Fatal("price-watch.yml has no ship step")
	}
	if apply >= ship {
		t.Error("the ship step runs before the apply step that tests the change")
	}
	run := steps[apply].Run
	for _, want := range []string{"-max-move", "tools/regold", "go test ./...", "scripts/smoke.sh"} {
		if !strings.Contains(run, want) {
			t.Errorf("the apply step does not run %q before anything ships", want)
		}
	}
	if !strings.Contains(steps[ship].If, "steps.apply.outcome == 'success'") {
		t.Error("the ship step does not gate on the apply step succeeding")
	}
}

// A price that failed a guard has to reach a person somewhere they will
// see it. A step summary on a green nightly run is not that place.
func TestPriceWatchFilesAnIssueForHeldPrices(t *testing.T) {
	w := readWorkflow(t, "price-watch.yml")
	hand := ""
	for _, st := range w.Jobs["diff"].Steps {
		if st.Name == "hand the rest to a person" {
			hand = st.If + st.Run
		}
	}
	if hand == "" {
		t.Fatal("price-watch.yml has no step that hands held prices to a person")
	}
	for _, want := range []string{"repointed", "held", "gh issue"} {
		if !strings.Contains(hand, want) {
			t.Errorf("the hand off step does not mention %q", want)
		}
	}
	if _, ok := w.Jobs["alert"]; !ok {
		t.Error("price-watch.yml has no alert job, so a failed nightly is invisible")
	}
}

// A push that carries a fix or a feature becomes a release with no
// person choosing the number. The tree is tested before it is tagged,
// the goldens are deliberately not regenerated on this path, and a
// major is refused rather than cut.
func TestAutoreleaseTestsBeforeItTags(t *testing.T) {
	w := readWorkflow(t, "autorelease.yml")
	steps := w.Jobs["decide"].Steps
	pos := map[string]int{}
	runs := map[string]string{}
	ifs := map[string]string{}
	for i, st := range steps {
		pos[st.Name] = i
		runs[st.Name] = st.Run
		ifs[st.Name] = st.If
	}
	for _, name := range []string{"read the commits since the last tag", "a major wants a person", "prepare the tree that will be tagged", "ship"} {
		if _, ok := pos[name]; !ok {
			t.Fatalf("autorelease.yml has no %q step", name)
		}
	}
	if pos["prepare the tree that will be tagged"] >= pos["ship"] {
		t.Error("ship runs before the tree is prepared and tested")
	}
	prep := runs["prepare the tree that will be tagged"]
	for _, want := range []string{"-classify", "-next-tag -bump", "tools/sync-docs", "go test ./...", "scripts/smoke.sh"} {
		if !strings.Contains(runs["read the commits since the last tag"]+prep, want) {
			t.Errorf("the prepare path does not run %q", want)
		}
	}
	if strings.Contains(prep, "regold") {
		t.Error("autorelease regenerates goldens, which erases the guard a code change is supposed to trip")
	}
	if !strings.Contains(ifs["ship"], "steps.prepare.outcome == 'success'") {
		t.Error("ship does not gate on prepare succeeding")
	}
	if !strings.Contains(ifs["a major wants a person"], "'major'") || !strings.Contains(runs["a major wants a person"], "gh issue") {
		t.Error("a major is not refused and filed for a person")
	}
	for _, name := range []string{"release", "image"} {
		job, ok := w.Jobs[name]
		if !ok || job.Uses != "./.github/workflows/"+name+".yml" || job.With["tag"] != "${{ needs.decide.outputs.tag }}" {
			t.Errorf("autorelease %s job does not call %s.yml with the decided tag", name, name)
		}
	}
	if _, ok := w.Jobs["alert"]; !ok {
		t.Error("autorelease.yml has no alert job, so a failed release is invisible")
	}
}

// Two workflows commit to main on the same push. They serialise on one
// concurrency group or the second push is rejected as non fast forward.
func TestMainWritersShareAConcurrencyGroup(t *testing.T) {
	groups := map[string]string{}
	for _, name := range []string{"autorelease.yml", "docs.yml"} {
		src := repoFile(t, ".github", "workflows", name)
		i := strings.Index(src, "group: ")
		if i < 0 {
			t.Fatalf("%s declares no concurrency group", name)
		}
		groups[name] = strings.Fields(src[i+len("group: "):])[0]
	}
	if groups["autorelease.yml"] != groups["docs.yml"] {
		t.Errorf("autorelease uses group %q and docs uses %q; they will race for main", groups["autorelease.yml"], groups["docs.yml"])
	}
}
