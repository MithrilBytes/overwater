package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/MithrilBytes/overwater/catalog"
)

// writeCatalogDir builds a two entry catalog source: test-model, whose
// prices litellm disagrees with, and loner-model, which litellm does
// not know at all.
func writeCatalogDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "models"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "VERSION"), []byte("2026-01-01\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	entries := map[string]string{
		"test-model":  "id: test-model\nprovider: testco\ninput_per_mtok: 1\noutput_per_mtok: 2\ncontext_window: 1000\ntier: mid\nreleased: \"2025-01-01\"\nsource: https://example.com/pricing\n",
		"loner-model": "id: loner-model\nprovider: testco\ninput_per_mtok: 5\noutput_per_mtok: 10\ncontext_window: 1000\ntier: mid\nreleased: \"2025-01-01\"\nsource: https://example.com/pricing\n",
	}
	for id, body := range entries {
		if err := os.WriteFile(filepath.Join(dir, "models", id+".yaml"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// The nightly price watch drives catalog diff. The whole happy path:
// report drift, then -write applies it, bumps VERSION, rebuilds
// catalog.json, and snapshots history.
func TestCatalogDiffDrift(t *testing.T) {
	dir := writeCatalogDir(t)
	litellm := filepath.Join(t.TempDir(), "litellm.json")
	// The window matches, so this is an ordinary repricing. A price that
	// moves together with the window is held back instead, which
	// TestCatalogDiffHoldsBackARepointedKey covers.
	prices := `{
  "test-model": {"input_cost_per_token": 2e-06, "output_cost_per_token": 4e-06, "max_input_tokens": 1000},
  "unrelated-model": {"input_cost_per_token": 1e-06}
}`
	if err := os.WriteFile(litellm, []byte(prices), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := Run([]string{"catalog", "diff", "-dir", dir, litellm}, &stdout, &stderr)
	if code != ExitClean {
		t.Fatalf("exit = %d, stderr = %q", code, stderr.String())
	}
	for _, want := range []string{
		"test-model: ours 1/2, litellm 2/4",
		"1 drifted, 0 repointed, 0 notes, 1 not in litellm, 2 checked",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("stdout is missing %q:\n%s", want, stdout.String())
		}
	}

	stdout.Reset()
	code = Run([]string{"catalog", "diff", "-dir", dir, "-write", litellm}, &stdout, &stderr)
	if code != ExitClean {
		t.Fatalf("-write exit = %d, stderr = %q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "updated 1 entries") {
		t.Errorf("stdout = %q, want the update count", stdout.String())
	}
	entry, err := os.ReadFile(filepath.Join(dir, "models", "test-model.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"input_per_mtok: 2\n", "output_per_mtok: 4\n"} {
		if !strings.Contains(string(entry), want) {
			t.Errorf("entry = %q, want %q applied", entry, want)
		}
	}
	rawVersion, err := os.ReadFile(filepath.Join(dir, "VERSION"))
	if err != nil {
		t.Fatal(err)
	}
	version := strings.TrimSpace(string(rawVersion))
	if !regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`).MatchString(version) || version == "2026-01-01" {
		t.Errorf("VERSION = %q, want it bumped to a fresh date", version)
	}
	built, err := os.ReadFile(filepath.Join(dir, "catalog.json"))
	if err != nil {
		t.Fatalf("catalog.json was not rebuilt: %v", err)
	}
	if !strings.Contains(string(built), `"test-model"`) || !strings.Contains(string(built), version) {
		t.Errorf("catalog.json = %q, want the entry at the bumped version", built)
	}
	if _, err := os.Stat(filepath.Join(dir, "history", version+".json")); err != nil {
		t.Errorf("history snapshot missing: %v", err)
	}

	// The applied prices agree with litellm now: a second diff is calm.
	stdout.Reset()
	if code := Run([]string{"catalog", "diff", "-dir", dir, litellm}, &stdout, &stderr); code != ExitClean {
		t.Fatalf("post-write exit = %d, stderr = %q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "0 drifted") {
		t.Errorf("stdout = %q, want no drift after the write", stdout.String())
	}
}

// writeCatalogHistory gives the two entry catalog two dated snapshots by
// applying real price changes, so the history under test is whatever
// ApplyPrices actually writes rather than hand written JSON. The second
// apply moves the input price only; the output price holds.
func writeCatalogHistory(t *testing.T) string {
	t.Helper()
	dir := writeCatalogDir(t)
	applies := []struct {
		version string
		drift   catalog.Drift
	}{
		{"2026-02-01", catalog.Drift{ID: "test-model", OursIn: 1, OursOut: 2, TheirsIn: 2, TheirsOut: 4, TheirsOutKnown: true}},
		{"2026-03-01", catalog.Drift{ID: "test-model", OursIn: 2, OursOut: 4, TheirsIn: 3, TheirsOut: 4, TheirsOutKnown: true}},
	}
	for _, a := range applies {
		if err := catalog.ApplyPrices(dir, []catalog.Drift{a.drift}, a.version); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// The snapshot list: every dated snapshot, its size, and how many prices
// moved into it.
func TestCatalogHistoryLists(t *testing.T) {
	dir := writeCatalogHistory(t)
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"catalog", "history", "-dir", dir}, &stdout, &stderr); code != ExitClean {
		t.Fatalf("exit = %d, stderr = %q", code, stderr.String())
	}
	for _, want := range []string{
		"2 snapshots, 2026-02-01 to 2026-03-01",
		"2026-02-01  2       -",
		"2026-03-01  2       1",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("stdout is missing %q:\n%s", want, stdout.String())
		}
	}
}

// What this model has cost over time, ending in the move across the
// whole span. Only the input price moved, so only it is named.
func TestCatalogHistoryModel(t *testing.T) {
	dir := writeCatalogHistory(t)
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"catalog", "history", "-dir", dir, "-model", "test-model"}, &stdout, &stderr); code != ExitClean {
		t.Fatalf("exit = %d, stderr = %q", code, stderr.String())
	}
	for _, want := range []string{
		"test-model across 2 snapshots",
		"2026-02-01  2          4",
		"2026-03-01  3          4",
		"in 2 -> 3 between 2026-02-01 and 2026-03-01",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("stdout is missing %q:\n%s", want, stdout.String())
		}
	}
	// An entry nobody has repriced reads as flat, not as absent history.
	stdout.Reset()
	if code := Run([]string{"catalog", "history", "-dir", dir, "-model", "loner-model"}, &stdout, &stderr); code != ExitClean {
		t.Fatalf("loner exit = %d, stderr = %q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "unchanged at 5/10 since 2026-02-01") {
		t.Errorf("stdout = %q, want the flat series called out", stdout.String())
	}
	// A name no snapshot carries is a bad invocation, not an empty report.
	stdout.Reset()
	stderr.Reset()
	if code := Run([]string{"catalog", "history", "-dir", dir, "-model", "ghost-model"}, &stdout, &stderr); code != ExitError {
		t.Fatalf("unknown model exit = %d, want %d", code, ExitError)
	}
	if !strings.Contains(stderr.String(), `carries "ghost-model"`) {
		t.Errorf("stderr = %q, want the unknown name reported", stderr.String())
	}
}

// What changed on a given date, against the snapshot before it.
func TestCatalogHistoryOnDate(t *testing.T) {
	dir := writeCatalogHistory(t)
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"catalog", "history", "-dir", dir, "-on", "2026-03-01"}, &stdout, &stderr); code != ExitClean {
		t.Fatalf("exit = %d, stderr = %q", code, stderr.String())
	}
	for _, want := range []string{
		"2026-03-01 against 2026-02-01",
		"test-model: in 2 -> 3",
		"1 moved, 0 added, 0 dropped",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("stdout is missing %q:\n%s", want, stdout.String())
		}
	}
	// The earliest snapshot has nothing behind it, which is a report, not
	// an error.
	stdout.Reset()
	if code := Run([]string{"catalog", "history", "-dir", dir, "-on", "2026-02-01"}, &stdout, &stderr); code != ExitClean {
		t.Fatalf("earliest exit = %d, stderr = %q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "2026-02-01 is the earliest snapshot") {
		t.Errorf("stdout = %q, want the earliest snapshot said so", stdout.String())
	}
	// A date with no snapshot names the dates there are.
	stdout.Reset()
	stderr.Reset()
	if code := Run([]string{"catalog", "history", "-dir", dir, "-on", "2026-02-14"}, &stdout, &stderr); code != ExitError {
		t.Fatalf("unknown date exit = %d, want %d", code, ExitError)
	}
	for _, want := range []string{"no snapshot dated 2026-02-14", "2026-03-01"} {
		if !strings.Contains(stderr.String(), want) {
			t.Errorf("stderr is missing %q:\n%s", want, stderr.String())
		}
	}
}

// An entry added between snapshots is an arrival, not a price move, and
// the two are counted apart.
func TestCatalogHistoryAddedEntry(t *testing.T) {
	dir := writeCatalogHistory(t)
	entry := "id: new-model\nprovider: testco\ninput_per_mtok: 7\noutput_per_mtok: 8\ncontext_window: 1000\ntier: mid\nreleased: \"2026-03-15\"\nsource: https://example.com/pricing\n"
	if err := os.WriteFile(filepath.Join(dir, "models", "new-model.yaml"), []byte(entry), 0o644); err != nil {
		t.Fatal(err)
	}
	drift := catalog.Drift{ID: "test-model", OursIn: 3, OursOut: 4, TheirsIn: 4, TheirsOut: 4, TheirsOutKnown: true}
	if err := catalog.ApplyPrices(dir, []catalog.Drift{drift}, "2026-04-01"); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"catalog", "history", "-dir", dir, "-on", "2026-04-01"}, &stdout, &stderr); code != ExitClean {
		t.Fatalf("exit = %d, stderr = %q", code, stderr.String())
	}
	for _, want := range []string{"new-model: added at 7/8", "test-model: in 3 -> 4", "1 moved, 1 added, 0 dropped"} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("stdout is missing %q:\n%s", want, stdout.String())
		}
	}
	stdout.Reset()
	if code := Run([]string{"catalog", "history", "-dir", dir}, &stdout, &stderr); code != ExitClean {
		t.Fatalf("list exit = %d, stderr = %q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "2026-04-01  3       1") {
		t.Errorf("stdout = %q, want the arrival in the model count and out of the price moves", stdout.String())
	}
}

// Only an applied price writes a snapshot, so a hand edited price with a
// bumped VERSION leaves the series short of the prices in the tree.
func TestCatalogHistoryNotesNewerVersion(t *testing.T) {
	dir := writeCatalogHistory(t)
	if err := os.WriteFile(filepath.Join(dir, "VERSION"), []byte("2026-04-01\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"catalog", "history", "-dir", dir}, &stdout, &stderr); code != ExitClean {
		t.Fatalf("exit = %d, stderr = %q", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "VERSION is 2026-04-01, newer than the last snapshot 2026-03-01") {
		t.Errorf("stderr = %q, want the short series noted", stderr.String())
	}
	if strings.Contains(stdout.String(), "2026-04-01") {
		t.Errorf("stdout = %q, want the note kept off the snapshot list", stdout.String())
	}
}

// A catalog nobody has repriced has no history to read, and the two
// questions cannot both be asked at once.
func TestCatalogHistoryRejects(t *testing.T) {
	dir := writeCatalogDir(t)
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"catalog", "history", "-dir", dir}, &stdout, &stderr); code != ExitError {
		t.Fatalf("empty history exit = %d, want %d", code, ExitError)
	}
	if !strings.Contains(stderr.String(), "no price history under") {
		t.Errorf("stderr = %q, want the empty history reported", stderr.String())
	}
	stderr.Reset()
	code := Run([]string{"catalog", "history", "-dir", dir, "-model", "test-model", "-on", "2026-02-01"}, &stdout, &stderr)
	if code != ExitError {
		t.Fatalf("both flags exit = %d, want %d", code, ExitError)
	}
	if !strings.Contains(stderr.String(), "-model or -on, not both") {
		t.Errorf("stderr = %q, want the flag pair rejected", stderr.String())
	}
}

// A drift free catalog with -write must not bump anything.
func TestCatalogDiffNoDrift(t *testing.T) {
	dir := writeCatalogDir(t)
	litellm := filepath.Join(t.TempDir(), "litellm.json")
	prices := `{"test-model": {"input_cost_per_token": 1e-06, "output_cost_per_token": 2e-06}}`
	if err := os.WriteFile(litellm, []byte(prices), 0o644); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"catalog", "diff", "-dir", dir, "-write", litellm}, &stdout, &stderr); code != ExitClean {
		t.Fatalf("exit = %d, stderr = %q", code, stderr.String())
	}
	raw, err := os.ReadFile(filepath.Join(dir, "VERSION"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(raw)) != "2026-01-01" {
		t.Errorf("VERSION = %q, want it untouched without drift", raw)
	}
	if _, err := os.Stat(filepath.Join(dir, "catalog.json")); !os.IsNotExist(err) {
		t.Errorf("catalog.json written without drift (stat err %v)", err)
	}
}

// A price that arrives with a new context window is upstream reusing an
// id for a new generation, not a repricing. Taking it silently priced
// mistral-medium-3 at Medium 3.5's rate, 3.75x high, and the nightly
// job opened a PR for it. It must be reported and must not be applied,
// including under -write.
func TestCatalogDiffHoldsBackARepointedKey(t *testing.T) {
	dir := writeCatalogDir(t)
	litellm := filepath.Join(t.TempDir(), "litellm.json")
	prices := `{
  "test-model": {"input_cost_per_token": 2e-06, "output_cost_per_token": 4e-06, "max_input_tokens": 9000}
}`
	if err := os.WriteFile(litellm, []byte(prices), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := Run([]string{"catalog", "diff", "-dir", dir, "-write", litellm}, &stdout, &stderr)
	if code != ExitClean {
		t.Fatalf("exit = %d, stderr = %q", code, stderr.String())
	}
	for _, want := range []string{
		"repointed: test-model: ours 1/2, litellm 2/4",
		"check whether the id still names our model",
		"0 drifted, 1 repointed,",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("stdout is missing %q:\n%s", want, stdout.String())
		}
	}
	entry, err := os.ReadFile(filepath.Join(dir, "models", "test-model.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(entry), "input_per_mtok: 1") {
		t.Errorf("-write applied a repointed price:\n%s", entry)
	}
}
