package release

import (
	"strings"
	"testing"
)

// price-watch is the only workflow that commits what it computed, and
// price-release turns a merged one into a tag and a release without a
// human in the loop. Neither runs until it fires for real, so the guards
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
		if strings.Contains(s.Run, "cp /tmp/unlisted.txt catalog/unlisted.txt") {
			run = s.Run
		}
	}
	if run == "" {
		t.Fatal("no price-watch step commits the roster; this test guards that copy")
	}
	guard := strings.Index(run, "[ ! -s /tmp/unlisted.txt ]")
	if guard < 0 {
		t.Fatal("price-watch copies /tmp/unlisted.txt over the roster without checking it carries anything")
	}
	if guard > strings.Index(run, "cp /tmp/unlisted.txt") {
		t.Error("the empty roster guard runs after the copy it exists to prevent")
	}
}

// The branch used to be the date alone, pushed with -f, so a second run
// in a day rewrote the ref an open PR was built from. A soft failing gh
// pr create left the branch pushed, no PR, and a green run to notice it.
func TestPriceWatchBranchIsPerRun(t *testing.T) {
	src := repoFile(t, ".github", "workflows", "price-watch.yml")
	for _, want := range []string{
		"concurrency:",
		"group: price-watch",
		`branch="$prefix/$(date +%Y-%m-%d)-$GITHUB_RUN_ID"`,
		"if ! gh pr create",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("price-watch.yml is missing %q", want)
		}
	}
	if strings.Contains(src, `|| echo "could not open a PR`) {
		t.Error("price-watch.yml still swallows a failed gh pr create")
	}
}

// The tag job releases main whatever the merged PR contained, and a head
// branch name is chosen by whoever opened the PR, not by price-watch.
func TestPriceReleaseGuardsMoreThanTheBranchName(t *testing.T) {
	src := repoFile(t, ".github", "workflows", "price-release.yml")
	for _, want := range []string{
		"github.event.pull_request.base.ref == 'main'",
		"github.event.pull_request.head.repo.full_name == github.repository",
		"github.event.pull_request.user.login == 'github-actions[bot]'",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("price-release.yml tag job does not require %s", want)
		}
	}
}
