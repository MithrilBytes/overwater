package catalog

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const sampleLitellm = `{
  "test-model": {"input_cost_per_token": 0.000002, "output_cost_per_token": 0.000008, "litellm_provider": "testco", "max_input_tokens": 1000},
  "testco/aliased-model": {"input_cost_per_token": 0.0000005, "output_cost_per_token": 0.000001},
  "window-model": {"input_cost_per_token": 0.000001, "output_cost_per_token": 0.000002, "litellm_provider": "testco", "max_input_tokens": 9000},
  "sample_spec": {"input_cost_per_token": "not a number"}
}`

func TestParseLitellmScalesPrices(t *testing.T) {
	prices, err := ParseLitellm([]byte(sampleLitellm))
	if err != nil {
		t.Fatal(err)
	}
	p, ok := prices["test-model"]
	if !ok || p.Input != 2 || p.Output != 8 {
		t.Fatalf("test-model = %+v, want 2 and 8 per million", p)
	}
	if _, ok := prices["sample_spec"]; ok {
		t.Error("malformed entry should be skipped")
	}
}

func diffFixtureCatalog() *Catalog {
	drifted := validModel()
	drifted.ID = "test-model"
	matching := validModel()
	matching.ID = "aliased-model"
	matching.Aliases = []string{"aliased-latest"}
	matching.InputPerMtok = 0.5
	matching.OutputPerMtok = 1
	absent := validModel()
	absent.ID = "not-tracked"
	retired := validModel()
	retired.ID = "old-model"
	retired.Deprecated = "2025-01-01"
	// Same price upstream, different window: a note and nothing more.
	window := validModel()
	window.ID = "window-model"
	return &Catalog{Version: "2026-01-01",
		Models: []Model{drifted, matching, absent, retired, window}}
}

func TestDiffLitellmTolerance(t *testing.T) {
	prices, err := ParseLitellm([]byte(sampleLitellm))
	if err != nil {
		t.Fatal(err)
	}
	d := DiffLitellm(diffFixtureCatalog(), prices, DiffOptions{MaxMove: 0})
	drifts, notes, missing := d.Drifts, d.Notes, d.Missing
	if len(drifts) != 1 || drifts[0].ID != "test-model" {
		t.Fatalf("drifts = %+v, want only test-model", drifts)
	}
	if drifts[0].TheirsIn != 2 || drifts[0].TheirsOut != 8 {
		t.Errorf("drift prices = %+v", drifts[0])
	}
	if len(notes) != 1 || !strings.Contains(notes[0], "context window") {
		t.Errorf("notes = %v, want the context window disagreement reported", notes)
	}
	// aliased-model matched on the provider prefixed key and its prices
	// agree, so it is neither drifted nor missing; the retired entry is
	// skipped.
	if len(missing) != 1 || missing[0] != "not-tracked" {
		t.Errorf("missing = %v, want only not-tracked", missing)
	}
}

// A drifted entry whose price line the regex cannot find fails the
// apply; VERSION stays put and no history snapshot is written.
func TestApplyPricesUnmatchedLine(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "models"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "VERSION"), []byte("2026-01-01\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// No space after the colon, so the price line regex cannot match.
	entry := "id: test-model\nprovider: testco\ninput_per_mtok:2\noutput_per_mtok: 3\n"
	if err := os.WriteFile(filepath.Join(dir, "models", "test-model.yaml"), []byte(entry), 0o644); err != nil {
		t.Fatal(err)
	}
	drift := Drift{ID: "test-model", TheirsIn: 4, TheirsOut: 8, TheirsOutKnown: true}
	err := ApplyPrices(dir, []Drift{drift}, "2026-08-06")
	if err == nil || !strings.Contains(err.Error(), "test-model.yaml") ||
		!strings.Contains(err.Error(), "input_per_mtok") {
		t.Fatalf("ApplyPrices() = %v, want an error naming the file and the unmatched line", err)
	}
	version, readErr := os.ReadFile(filepath.Join(dir, "VERSION"))
	if readErr != nil {
		t.Fatal(readErr)
	}
	if strings.TrimSpace(string(version)) != "2026-01-01" {
		t.Errorf("VERSION = %q, want it untouched after the failure", version)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "history")); !os.IsNotExist(statErr) {
		t.Error("history snapshot written despite the failed apply")
	}

	// The same guard covers the output line.
	entry = "id: test-model\nprovider: testco\ninput_per_mtok: 2\noutput_per_mtok:3\n"
	if err := os.WriteFile(filepath.Join(dir, "models", "test-model.yaml"), []byte(entry), 0o644); err != nil {
		t.Fatal(err)
	}
	err = ApplyPrices(dir, []Drift{drift}, "2026-08-06")
	if err == nil || !strings.Contains(err.Error(), "output_per_mtok") {
		t.Fatalf("ApplyPrices() = %v, want the unmatched output line reported", err)
	}
}

// An upstream record with input but no output cost must not read as
// "output is now free": ours is not compared, drifted, or applied over.
func TestDiffLitellmMissingOutput(t *testing.T) {
	c := &Catalog{Version: "2026-01-01", Models: []Model{func() Model {
		m := validModel()
		m.ID = "test-model" // ours: input 1, output 2
		return m
	}()}}

	// Upstream input agrees, output absent: no drift at all.
	prices, err := ParseLitellm([]byte(`{"test-model": {"input_cost_per_token": 0.000001}}`))
	if err != nil {
		t.Fatal(err)
	}
	if p := prices["test-model"]; p.HasOutput {
		t.Fatalf("parsed entry claims an output price it does not have: %+v", p)
	}
	drifts := DiffLitellm(c, prices, DiffOptions{MaxMove: 0}).Drifts
	if len(drifts) != 0 {
		t.Fatalf("drifts = %+v, want none when only the absent output differs", drifts)
	}

	// Upstream input drifts, output still absent: the drift carries the
	// input change and marks the output unknown, and applying it leaves
	// our output price in place.
	prices, err = ParseLitellm([]byte(`{"test-model": {"input_cost_per_token": 0.000003}}`))
	if err != nil {
		t.Fatal(err)
	}
	drifts = DiffLitellm(c, prices, DiffOptions{MaxMove: 0}).Drifts
	if len(drifts) != 1 || drifts[0].TheirsIn != 3 || drifts[0].TheirsOutKnown {
		t.Fatalf("drifts = %+v, want one input-only drift with TheirsOutKnown false", drifts)
	}
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "models"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "VERSION"), []byte("2026-01-01\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	entry := "id: test-model\nprovider: testco\ninput_per_mtok: 1\noutput_per_mtok: 2\ncontext_window: 1000\ntier: mid\nreleased: \"2025-01-01\"\nsource: https://example.com/pricing\n"
	if err := os.WriteFile(filepath.Join(dir, "models", "test-model.yaml"), []byte(entry), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ApplyPrices(dir, drifts, "2026-08-06"); err != nil {
		t.Fatal(err)
	}
	updated, err := os.ReadFile(filepath.Join(dir, "models", "test-model.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(updated), "input_per_mtok: 3\n") ||
		!strings.Contains(string(updated), "output_per_mtok: 2\n") {
		t.Errorf("entry = %s, want input applied and output untouched", updated)
	}
}

func TestApplyPrices(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "models"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "VERSION"), []byte("2026-01-01\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	entry := "id: test-model\nprovider: testco\ninput_per_mtok: 1\noutput_per_mtok: 2\ncontext_window: 1000\ntier: mid\nreleased: \"2025-01-01\"\nsource: https://example.com/pricing\n"
	if err := os.WriteFile(filepath.Join(dir, "models", "test-model.yaml"), []byte(entry), 0o644); err != nil {
		t.Fatal(err)
	}
	drift := Drift{ID: "test-model", TheirsIn: 2, TheirsOut: 8, TheirsOutKnown: true}
	if err := ApplyPrices(dir, []Drift{drift}, "2026-08-05"); err != nil {
		t.Fatal(err)
	}
	updated, err := os.ReadFile(filepath.Join(dir, "models", "test-model.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(updated), "input_per_mtok: 2\n") ||
		!strings.Contains(string(updated), "output_per_mtok: 8\n") {
		t.Errorf("entry not rewritten: %s", updated)
	}
	version, err := os.ReadFile(filepath.Join(dir, "VERSION"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(version)) != "2026-08-05" {
		t.Errorf("VERSION = %q", version)
	}
	rebuilt, err := os.ReadFile(filepath.Join(dir, "catalog.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(rebuilt), `"input_per_mtok": 2`) {
		t.Errorf("catalog.json not rebuilt: %s", rebuilt)
	}
}

// Upstream repoints a -latest key to each new generation, so a pinned
// entry matched against one reads the successor's price as our drift.
func TestDiffSkipsFloatingAliases(t *testing.T) {
	m := validModel()
	m.ID = "mistral-medium-3"
	m.Provider = "mistral"
	m.InputPerMtok, m.OutputPerMtok = 0.40, 2.00
	m.Aliases = []string{"mistral-medium-latest", "mistral-medium-2505"}
	c := &Catalog{Version: "2026-01-01", Models: []Model{m}}

	prices := LitellmPrices{
		// The floating alias now points at the next generation.
		"mistral/mistral-medium-latest": {Input: 1.5, Output: 7.5, HasOutput: true},
		"mistral/mistral-medium-2505":   {Input: 0.40, Output: 2.00, HasOutput: true},
	}
	d := DiffLitellm(c, prices, DiffOptions{MaxMove: 0})
	drifts, missing := d.Drifts, d.Missing
	if len(drifts) != 0 {
		t.Fatalf("drifts = %+v, want none; the pinned key agrees with us", drifts)
	}
	if len(missing) != 0 {
		t.Fatalf("missing = %v, want none; the dated alias still matches", missing)
	}
}

// Cache rates are published as multiples of base input, so an applied
// price change has to carry them along or they describe the old price.
func TestApplyPricesScalesCacheRates(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "models"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "VERSION"), []byte("2026-01-01\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	entry := "id: test-model\nprovider: testco\ninput_per_mtok: 3.00\noutput_per_mtok: 15.00\n" +
		"cache_read_per_mtok: 0.3\ncache_write_per_mtok: 3.75\ncontext_window: 1000\n" +
		"tier: mid\nreleased: \"2025-01-01\"\nsource: https://example.com/pricing\n"
	if err := os.WriteFile(filepath.Join(dir, "models", "test-model.yaml"), []byte(entry), 0o644); err != nil {
		t.Fatal(err)
	}
	drift := Drift{ID: "test-model", OursIn: 3, OursOut: 15, TheirsIn: 2, TheirsOut: 10, TheirsOutKnown: true}
	if err := ApplyPrices(dir, []Drift{drift}, "2026-08-08"); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(dir, "models", "test-model.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"input_per_mtok: 2\n", "output_per_mtok: 10\n",
		"cache_read_per_mtok: 0.2\n", "cache_write_per_mtok: 2.5\n",
	} {
		if !strings.Contains(string(got), want) {
			t.Errorf("entry missing %q:\n%s", want, got)
		}
	}
}

// Upstream reuses a key when a family ships a new generation, and the
// giveaway is that the context window moves with the price. LiteLLM
// repointed mistral-medium-3 to Medium 3.5 at 1.5/7.5 over a 262144
// window while the model this catalog describes stayed at 0.4/2 over
// 131072 under its dated id. The nightly job applied the price and
// opened a PR that would have overstated every call site by 3.75x.
func TestRepointedKeyIsNotAppliedAsDrift(t *testing.T) {
	m := validModel()
	m.ID = "mistral-medium-3"
	m.Provider = "mistral"
	m.InputPerMtok, m.OutputPerMtok = 0.4, 2
	m.ContextWindow = 131072
	c := &Catalog{Version: "2026-01-01", Models: []Model{m}}

	repricedInPlace := LitellmPrices{
		"mistral-medium-3": {Input: 0.5, Output: 2.5, HasOutput: true, MaxInput: 131072, Mode: "chat"},
	}
	r := DiffLitellm(c, repricedInPlace, DiffOptions{MaxMove: 0})
	drifts, repointed := r.Drifts, r.Repointed
	if len(drifts) != 1 || len(repointed) != 0 {
		t.Errorf("a price move at the same window is ordinary drift; got %d drift, %d repointed",
			len(drifts), len(repointed))
	}

	newGenerationUnderTheSameKey := LitellmPrices{
		"mistral-medium-3": {Input: 1.5, Output: 7.5, HasOutput: true, MaxInput: 262144, Mode: "chat"},
	}
	g := DiffLitellm(c, newGenerationUnderTheSameKey, DiffOptions{MaxMove: 0})
	drifts, repointed, notes := g.Drifts, g.Repointed, g.Notes
	if len(drifts) != 0 {
		t.Errorf("a price that moved with the window was offered as applyable drift: %+v", drifts)
	}
	if len(repointed) != 1 || repointed[0].ID != "mistral-medium-3" {
		t.Fatalf("repointed = %+v, want the one entry held back", repointed)
	}
	if repointed[0].TheirsIn != 1.5 {
		t.Errorf("held back entry lost the upstream price: %+v", repointed[0])
	}
	// The window disagreement still has to be said out loud.
	if len(notes) == 0 {
		t.Error("no note named the context window disagreement")
	}
}

// 128000 and 131072 are one window written two ways, and treating that
// as a repoint would hold back every real repricing on a model whose
// upstream rounds differently. A doubling is a different model.
func TestWindowToleranceSeparatesRoundingFromRepointing(t *testing.T) {
	m := validModel()
	m.ID = "tolerant"
	m.ContextWindow = 131072
	c := &Catalog{Version: "2026-01-01", Models: []Model{m}}

	rounded := LitellmPrices{"tolerant": {Input: 2, Output: 4, HasOutput: true, MaxInput: 128000, Mode: "chat"}}
	if d := DiffLitellm(c, rounded, DiffOptions{MaxMove: 0}); len(d.Drifts) != 1 || len(d.Repointed) != 0 {
		t.Errorf("a rounded window read as a repoint: drifts %d, repointed %d", len(d.Drifts), len(d.Repointed))
	}
	doubled := LitellmPrices{"tolerant": {Input: 2, Output: 4, HasOutput: true, MaxInput: 262144, Mode: "chat"}}
	if d := DiffLitellm(c, doubled, DiffOptions{MaxMove: 0}); len(d.Drifts) != 0 || len(d.Repointed) != 1 {
		t.Errorf("a doubled window did not read as a repoint: drifts %d, repointed %d", len(d.Drifts), len(d.Repointed))
	}
}

// grok-4 was cut to 1.25/2.5 from 3/15, a real change that a person
// should look at before it ships on its own. Below the factor the
// nightly job applies the move; above it the move waits.
func TestLargeMovesAreHeldForAPerson(t *testing.T) {
	m := validModel()
	m.ID = "swing"
	m.InputPerMtok, m.OutputPerMtok = 3, 15
	c := &Catalog{Version: "2026-01-01", Models: []Model{m}}
	prices := LitellmPrices{"swing": {Input: 1.25, Output: 2.5, HasOutput: true, MaxInput: 1000, Mode: "chat"}}

	if d := DiffLitellm(c, prices, DiffOptions{MaxMove: 0}); len(d.Drifts) != 1 || len(d.Held) != 0 {
		t.Errorf("with no cap the move should apply: drifts %d, held %d", len(d.Drifts), len(d.Held))
	}
	if d := DiffLitellm(c, prices, DiffOptions{MaxMove: 3}); len(d.Drifts) != 0 || len(d.Held) != 1 {
		t.Errorf("a 6x move under a 3x cap should be held: drifts %d, held %d", len(d.Drifts), len(d.Held))
	}
	if d := DiffLitellm(c, prices, DiffOptions{MaxMove: 10}); len(d.Drifts) != 1 || len(d.Held) != 0 {
		t.Errorf("a 6x move under a 10x cap should apply: drifts %d, held %d", len(d.Drifts), len(d.Held))
	}
}

// Nine entries sat retired upstream and active here, and the tool
// recommended one of them six months after it shut off. A date is a
// fact from the provider, like a price, and is applied like one.
func TestDeprecationDatesAreAppliedWhenTheWindowMatches(t *testing.T) {
	m := validModel()
	m.ID = "retiring"
	c := &Catalog{Version: "2026-01-01", Models: []Model{m}}

	same := LitellmPrices{"retiring": {Input: 1, Output: 2, HasOutput: true, MaxInput: 1000, Mode: "chat", Deprecation: "2026-10-23"}}
	d := DiffLitellm(c, same, DiffOptions{MaxMove: 0})
	if len(d.Deprecations) != 1 || d.Deprecations[0].Date != "2026-10-23" {
		t.Fatalf("deprecations = %+v, want the upstream date", d.Deprecations)
	}
	// A repointed key's date is for whatever the key names now.
	moved := LitellmPrices{"retiring": {Input: 1, Output: 2, HasOutput: true, MaxInput: 9000, Mode: "chat", Deprecation: "2026-10-23"}}
	if d := DiffLitellm(c, moved, DiffOptions{MaxMove: 0}); len(d.Deprecations) != 0 {
		t.Errorf("a repointed key's deprecation was applied: %+v", d.Deprecations)
	}

	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "models"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "VERSION"), []byte("2026-01-01\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	entry := "id: retiring\nprovider: testco\ninput_per_mtok: 1\noutput_per_mtok: 2\ncontext_window: 1000\ntier: mid\nreleased: \"2025-01-01\"\nsource: https://example.com\n"
	path := filepath.Join(dir, "models", "retiring.yaml")
	if err := os.WriteFile(path, []byte(entry), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ApplyDeprecations(dir, d.Deprecations); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(path)
	want := "released: \"2025-01-01\"\ndeprecated: \"2026-10-23\"\n"
	if !strings.Contains(string(got), want) {
		t.Errorf("entry after apply:\n%s\nwant the date after released", got)
	}
	// Applying it again replaces rather than duplicates.
	if err := ApplyDeprecations(dir, []Deprecation{{ID: "retiring", Date: "2027-01-01"}}); err != nil {
		t.Fatal(err)
	}
	got, _ = os.ReadFile(path)
	if strings.Count(string(got), "deprecated:") != 1 || !strings.Contains(string(got), `"2027-01-01"`) {
		t.Errorf("second apply did not replace the line:\n%s", got)
	}
	// And the result still validates as a catalog entry.
	if _, err := LoadDir(dir); err != nil {
		t.Errorf("applied entry does not load: %v", err)
	}
}

// An entry with a retirement date still ahead of it is live, and its
// price has to keep syncing until the date. Treating any set date as
// retired would have frozen the current lineup's prices the night the
// published dates were applied.
func TestFutureDatedEntriesStillSync(t *testing.T) {
	today := time.Date(2026, 9, 5, 0, 0, 0, 0, time.UTC)
	live := validModel()
	live.ID = "still-live"
	live.Deprecated = "2027-07-24"
	gone := validModel()
	gone.ID = "already-gone"
	gone.Deprecated = "2026-02-28"
	c := &Catalog{Version: "2026-01-01", Models: []Model{live, gone}}
	prices := LitellmPrices{
		"still-live":   {Input: 2, Output: 4, HasOutput: true, MaxInput: 1000, Mode: "chat"},
		"already-gone": {Input: 2, Output: 4, HasOutput: true, MaxInput: 1000, Mode: "chat"},
	}
	d := DiffLitellm(c, prices, DiffOptions{Today: today})
	if len(d.Drifts) != 1 || d.Drifts[0].ID != "still-live" {
		t.Errorf("drifts = %+v, want only the live entry to sync", d.Drifts)
	}
}
