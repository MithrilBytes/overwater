package catalog

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// historyDir applies two price changes to a one entry catalog, so the
// snapshots under test are the ones ApplyPrices actually writes.
func historyDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "models"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "VERSION"), []byte("2026-01-01\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	entry := "id: test-model\nprovider: testco\naliases:\n  - test-latest\ninput_per_mtok: 1\noutput_per_mtok: 2\n" +
		"context_window: 1000\ntier: mid\nreleased: \"2025-01-01\"\nsource: https://example.com/pricing\n"
	if err := os.WriteFile(filepath.Join(dir, "models", "test-model.yaml"), []byte(entry), 0o644); err != nil {
		t.Fatal(err)
	}
	applies := []struct {
		version string
		drift   Drift
	}{
		{"2026-02-01", Drift{ID: "test-model", OursIn: 1, OursOut: 2, TheirsIn: 2, TheirsOut: 4, TheirsOutKnown: true}},
		{"2026-03-01", Drift{ID: "test-model", OursIn: 2, OursOut: 4, TheirsIn: 3, TheirsOut: 4, TheirsOutKnown: true}},
	}
	for _, a := range applies {
		if err := ApplyPrices(dir, []Drift{a.drift}, a.version); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestLoadHistoryReadsApplied(t *testing.T) {
	snaps, err := LoadHistory(historyDir(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(snaps) != 2 {
		t.Fatalf("snapshots = %d, want the two applies", len(snaps))
	}
	if snaps[0].Catalog.Version != "2026-02-01" || snaps[1].Catalog.Version != "2026-03-01" {
		t.Fatalf("versions = %s, %s, want them oldest first", snaps[0].Catalog.Version, snaps[1].Catalog.Version)
	}
	if got := snaps[1].Catalog.Models[0].InputPerMtok; got != 3 {
		t.Errorf("newest input price = %g, want the second applied price", got)
	}
}

// A catalog with no applied price has no history, which the reader
// reports as nothing rather than as a failure.
func TestLoadHistoryEmpty(t *testing.T) {
	snaps, err := LoadHistory(t.TempDir())
	if err != nil || len(snaps) != 0 {
		t.Fatalf("LoadHistory() = %v, %v, want no snapshots and no error", snaps, err)
	}
}

// Something else dropped into history/ is an error naming the file, not
// a silently skipped date.
func TestLoadHistoryRejectsNonSnapshot(t *testing.T) {
	dir := historyDir(t)
	path := filepath.Join(dir, "history", "notes.json")
	if err := os.WriteFile(path, []byte(`{"models": []}`), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := LoadHistory(dir)
	if err == nil || !strings.Contains(err.Error(), "notes.json") {
		t.Fatalf("LoadHistory() = %v, want an error naming the file", err)
	}
}

func TestChangesBetween(t *testing.T) {
	moved := validModel()
	moved.InputPerMtok, moved.OutputPerMtok = 1, 2
	held := validModel()
	held.ID = "steady-model"
	leaving := validModel()
	leaving.ID = "old-model"
	prev := &Catalog{Version: "2026-02-01", Models: []Model{moved, held, leaving}}

	repriced := moved
	repriced.OutputPerMtok = 6 // input holds, so only the output side moved
	arriving := validModel()
	arriving.ID = "new-model"
	arriving.InputPerMtok, arriving.OutputPerMtok = 7, 8
	cur := &Catalog{Version: "2026-03-01", Models: []Model{repriced, held, arriving}}

	changes := ChangesBetween(prev, cur)
	if len(changes) != 3 {
		t.Fatalf("changes = %+v, want the move, the arrival, and the departure", changes)
	}
	// Sorted by id: new-model, old-model, test-model.
	if c := changes[0]; !c.Added || c.ID != "new-model" || c.NewIn != 7 {
		t.Errorf("changes[0] = %+v, want new-model added at 7", c)
	}
	if c := changes[1]; !c.Dropped || c.ID != "old-model" || c.OldIn != 1 {
		t.Errorf("changes[1] = %+v, want old-model dropped", c)
	}
	if c := changes[2]; c.Added || c.Dropped || c.OldIn != c.NewIn || c.OldOut != 2 || c.NewOut != 6 {
		t.Errorf("changes[2] = %+v, want test-model's output price moved alone", c)
	}
}

// A series follows the name through the snapshots that carry it, and
// reports what it resolved to: an alias repointed at a new generation is
// a different entry, not a price cut.
func TestSeriesFollowsAliases(t *testing.T) {
	first := validModel()
	first.Aliases = []string{"test-latest"}
	next := validModel()
	next.ID = "test-model-2"
	next.Aliases = []string{"test-latest"}
	next.InputPerMtok = 9
	snaps := []Snapshot{
		{Catalog: &Catalog{Version: "2026-01-01", Models: []Model{validModel()}}},
		{Catalog: &Catalog{Version: "2026-02-01", Models: []Model{first}}},
		{Catalog: &Catalog{Version: "2026-03-01", Models: []Model{next}}},
	}
	points := Series(snaps, "test-latest")
	if len(points) != 2 {
		t.Fatalf("points = %+v, want the snapshots carrying the alias", points)
	}
	if points[0].ID != "test-model" || points[0].In != 1 {
		t.Errorf("points[0] = %+v, want the original entry", points[0])
	}
	if points[1].ID != "test-model-2" || points[1].In != 9 {
		t.Errorf("points[1] = %+v, want the repointed entry named", points[1])
	}
}
