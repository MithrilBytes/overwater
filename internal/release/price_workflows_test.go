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
