package cli

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func gitOrSkip(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not on PATH")
	}
}

func gitRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// initRepo turns dir into a git repository with one commit of whatever
// is already in it.
func initRepo(t *testing.T, dir string) {
	t.Helper()
	gitRun(t, dir, "init", "-q")
	gitRun(t, dir, "config", "user.email", "test@example.com")
	gitRun(t, dir, "config", "user.name", "Test")
	gitRun(t, dir, "config", "commit.gpgsign", "false")
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-q", "-m", "base")
}

func readBaseline(t *testing.T, path string) (string, []string) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Commit   string `json:"commit"`
		Findings []struct {
			File string `json:"file"`
		} `json:"findings"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	var files []string
	for _, f := range doc.Findings {
		files = append(files, f.File)
	}
	return doc.Commit, files
}

// --update-baseline records the repo's HEAD, and --incremental then
// scans only what git reports changed: the untracked file's finding is
// counted and the committed baselined files are never rescanned.
func TestIncrementalScansChangedFiles(t *testing.T) {
	gitOrSkip(t)
	dir := t.TempDir()
	repo := filepath.Join(dir, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	writeRepoFile(t, repo, "classify.js", classifyCall)
	writeRepoFile(t, repo, "legacy.js", legacyCall)
	initRepo(t, repo)

	bl := filepath.Join(dir, ".overwater.json")
	if code, _, stderr := runScanArgs(t, "-baseline", bl, "-update-baseline", repo); code != ExitClean {
		t.Fatalf("update exit = %d, stderr = %q", code, stderr)
	}
	commit, files := readBaseline(t, bl)
	if len(commit) < 7 {
		t.Fatalf("baseline commit = %q, want a git sha", commit)
	}
	if len(files) < 2 {
		t.Fatalf("baselined files = %v, want findings from both committed files", files)
	}

	writeRepoFile(t, repo, "fresh.js", legacyCall)
	code, _, stderr := runScanArgs(t, "-baseline", bl, "-incremental", repo)
	if code != ExitFindings {
		t.Fatalf("exit = %d, want %d; stderr = %q", code, ExitFindings, stderr)
	}
	if !strings.Contains(stderr, "1 findings, 1 new") {
		t.Errorf("stderr = %q, want only the changed file's finding counted", stderr)
	}
	if !strings.Contains(stderr, "at fresh.js") {
		t.Errorf("stderr = %q, want the new finding named in fresh.js", stderr)
	}
}

// Baselined findings in unscanned files are assumed unchanged: when
// only an unrelated file changes, the ratchet stays green.
func TestIncrementalRatchetStaysGreen(t *testing.T) {
	gitOrSkip(t)
	dir := t.TempDir()
	repo := filepath.Join(dir, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	writeRepoFile(t, repo, "classify.js", classifyCall)
	initRepo(t, repo)

	bl := filepath.Join(dir, ".overwater.json")
	if code, _, stderr := runScanArgs(t, "-baseline", bl, "-update-baseline", repo); code != ExitClean {
		t.Fatalf("update exit = %d, stderr = %q", code, stderr)
	}
	writeRepoFile(t, repo, "README.md", "docs only\n")
	code, _, stderr := runScanArgs(t, "-baseline", bl, "-incremental", repo)
	if code != ExitClean {
		t.Fatalf("exit = %d, stderr = %q; unscanned baselined findings misfired the ratchet", code, stderr)
	}
}

// An incremental --update-baseline keeps the entries for files it never
// scanned instead of pruning them.
func TestIncrementalUpdateKeepsUnscanned(t *testing.T) {
	gitOrSkip(t)
	dir := t.TempDir()
	repo := filepath.Join(dir, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	writeRepoFile(t, repo, "classify.js", classifyCall)
	writeRepoFile(t, repo, "legacy.js", legacyCall)
	initRepo(t, repo)

	bl := filepath.Join(dir, ".overwater.json")
	if code, _, stderr := runScanArgs(t, "-baseline", bl, "-update-baseline", repo); code != ExitClean {
		t.Fatalf("update exit = %d, stderr = %q", code, stderr)
	}
	writeRepoFile(t, repo, "fresh.js", legacyCall)
	if code, _, stderr := runScanArgs(t, "-baseline", bl, "-incremental", "-update-baseline", repo); code != ExitClean {
		t.Fatalf("incremental update exit = %d, stderr = %q", code, stderr)
	}
	_, files := readBaseline(t, bl)
	for _, want := range []string{"classify.js", "legacy.js", "fresh.js"} {
		found := false
		for _, f := range files {
			if f == want {
				found = true
			}
		}
		if !found {
			t.Errorf("baseline files = %v, missing %s", files, want)
		}
	}
}

// Every incremental scan says how many files it covered, so a null
// verdict over zero files cannot read as a clean bill of health.
func TestIncrementalCoverageNote(t *testing.T) {
	gitOrSkip(t)
	dir := t.TempDir()
	repo := filepath.Join(dir, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	writeRepoFile(t, repo, "classify.js", classifyCall)
	initRepo(t, repo)
	bl := filepath.Join(dir, ".overwater.json")
	if code, _, stderr := runScanArgs(t, "-baseline", bl, "-update-baseline", repo); code != ExitClean {
		t.Fatalf("update exit = %d, stderr = %q", code, stderr)
	}

	// Nothing changed: the scan covered zero files and says so, while
	// stdout still carries the null verdict.
	code, stdout, stderr := runScanArgs(t, "-baseline", bl, "-incremental", repo)
	if code != ExitClean {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
	if !strings.Contains(stderr, "incremental: scanned 0 of 0 candidate files") {
		t.Errorf("stderr = %q, want the zero coverage note", stderr)
	}
	if !strings.Contains(stdout, "Keep the models you have.") {
		t.Errorf("stdout = %q, want the null verdict untouched", stdout)
	}

	// One new file: one of one.
	writeRepoFile(t, repo, "fresh.js", legacyCall)
	code, _, stderr = runScanArgs(t, "-baseline", bl, "-incremental", repo)
	if code != ExitFindings {
		t.Fatalf("exit = %d, want %d; stderr = %q", code, ExitFindings, stderr)
	}
	if !strings.Contains(stderr, "incremental: scanned 1 of 1 candidate files") {
		t.Errorf("stderr = %q, want a one of one note", stderr)
	}

	// A deleted file is a candidate git names but nothing to scan.
	if err := os.Remove(filepath.Join(repo, "fresh.js")); err != nil {
		t.Fatal(err)
	}
	gitRun(t, repo, "rm", "-q", "classify.js")
	code, _, stderr = runScanArgs(t, "-baseline", bl, "-incremental", repo)
	if code != ExitClean {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
	if !strings.Contains(stderr, "incremental: scanned 0 of 1 candidate files") {
		t.Errorf("stderr = %q, want a zero of one note for the deleted file", stderr)
	}
}

// Under the default core.quotePath, git wraps any path holding a non
// ASCII byte in quotes and octal escapes it, and that string matches no
// real file: --incremental used to drop the file and report a clean run
// over a live deprecated model call. NUL terminated listings carry the
// raw bytes the walker sees.
func TestIncrementalFindsNonASCIIPaths(t *testing.T) {
	gitOrSkip(t)
	dir := t.TempDir()
	repo := filepath.Join(dir, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	writeRepoFile(t, repo, "classify.js", classifyCall)
	initRepo(t, repo)
	gitRun(t, repo, "config", "core.quotePath", "true")
	// Report the filesystem's own bytes, so the assertion holds on a
	// volume that decomposes what we wrote.
	gitRun(t, repo, "config", "core.precomposeunicode", "false")

	bl := filepath.Join(dir, ".overwater.json")
	if code, _, stderr := runScanArgs(t, "-baseline", bl, "-update-baseline", repo); code != ExitClean {
		t.Fatalf("update exit = %d, stderr = %q", code, stderr)
	}

	// One committed and one untracked: git names the first through diff
	// and the second through ls-files, and both quote by default.
	writeRepoFile(t, repo, "naïve.js", legacyCall)
	gitRun(t, repo, "add", "-A")
	gitRun(t, repo, "commit", "-q", "-m", "naive")
	writeRepoFile(t, repo, "café.js", legacyCall)

	code, _, stderr := runScanArgs(t, "-baseline", bl, "-incremental", repo)
	if code != ExitFindings {
		t.Fatalf("exit = %d, want %d; a missed file in the guard reads as a clean bill of health; stderr = %q",
			code, ExitFindings, stderr)
	}
	if !strings.Contains(stderr, "incremental: scanned 2 of 2 candidate files") {
		t.Errorf("stderr = %q, want both non ASCII paths counted", stderr)
	}
	for _, want := range []string{"new: deprecated-model at naïve.js", "new: deprecated-model at café.js"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("stderr = %q, want %q", stderr, want)
		}
	}
}

// Without a recorded commit the scan falls back to a full one with a
// single stderr note. No git repository is needed to exercise this.
func TestIncrementalFallsBackWithoutCommit(t *testing.T) {
	dir := t.TempDir()
	repo := filepath.Join(dir, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	writeRepoFile(t, repo, "classify.js", classifyCall)

	bl := filepath.Join(dir, ".overwater.json")
	if code, _, stderr := runScanArgs(t, "-baseline", bl, "-update-baseline", repo); code != ExitClean {
		t.Fatalf("update exit = %d, stderr = %q", code, stderr)
	}
	code, _, stderr := runScanArgs(t, "-baseline", bl, "-incremental", repo)
	if code != ExitClean {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
	if !strings.Contains(stderr, "scanning everything") {
		t.Errorf("stderr = %q, want a fallback note", stderr)
	}
	if !strings.Contains(stderr, "all baselined") {
		t.Errorf("stderr = %q, want the full scan to match the baseline", stderr)
	}
}
