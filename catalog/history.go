package catalog

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// Reading the dated snapshots ApplyPrices writes under history/. The
// series is exactly the price watch's record: a date with no snapshot is
// a date on which no price moved.

// Snapshot is one dated catalog under history/. Path rides along for
// error messages; the date is the catalog's own Version, which is also
// the file name ApplyPrices chose.
type Snapshot struct {
	Path    string
	Catalog *Catalog
}

// LoadHistory reads dir/history/*.json, oldest first. No history is not
// an error here: a catalog whose prices have never been applied has none,
// and the caller decides what to say about that.
func LoadHistory(dir string) ([]Snapshot, error) {
	paths, err := filepath.Glob(filepath.Join(dir, "history", "*.json"))
	if err != nil {
		return nil, err
	}
	var snaps []Snapshot
	for _, path := range paths {
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		var c Catalog
		if err := json.Unmarshal(raw, &c); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		// Validate, not called here, would reject an old snapshot over a
		// rule the catalog grew afterwards, and old history is still
		// history. The version is the one field a reader cannot do without.
		if c.Version == "" {
			return nil, fmt.Errorf("%s: not a catalog snapshot; it carries no version", path)
		}
		snaps = append(snaps, Snapshot{Path: path, Catalog: &c})
	}
	sort.Slice(snaps, func(i, j int) bool { return snaps[i].Catalog.Version < snaps[j].Catalog.Version })
	return snaps, nil
}

// PriceChange is one entry's prices moving between two snapshots, or the
// entry arriving or leaving. Cache rates are absent by design: ApplyPrices
// scales them with the input price, so reporting them repeats the input
// line in different numbers.
type PriceChange struct {
	ID             string
	OldIn, OldOut  float64
	NewIn, NewOut  float64
	Added, Dropped bool
}

// ChangesBetween reports what moved from prev to cur, sorted by id. Both
// sides were written by the same builder from the same YAML, so the
// prices compare exactly; the tolerance in DiffLitellm exists for
// upstream float dust, which never reaches a snapshot.
func ChangesBetween(prev, cur *Catalog) []PriceChange {
	before, after := byID(prev), byID(cur)
	var changes []PriceChange
	for id, m := range after {
		old, existed := before[id]
		switch {
		case !existed:
			changes = append(changes, PriceChange{ID: id, NewIn: m.InputPerMtok, NewOut: m.OutputPerMtok, Added: true})
		case old.InputPerMtok != m.InputPerMtok || old.OutputPerMtok != m.OutputPerMtok:
			changes = append(changes, PriceChange{
				ID: id, OldIn: old.InputPerMtok, OldOut: old.OutputPerMtok,
				NewIn: m.InputPerMtok, NewOut: m.OutputPerMtok,
			})
		}
	}
	for id, m := range before {
		if _, kept := after[id]; !kept {
			changes = append(changes, PriceChange{ID: id, OldIn: m.InputPerMtok, OldOut: m.OutputPerMtok, Dropped: true})
		}
	}
	sort.Slice(changes, func(i, j int) bool { return changes[i].ID < changes[j].ID })
	return changes
}

func byID(c *Catalog) map[string]Model {
	m := make(map[string]Model, len(c.Models))
	for _, entry := range c.Models {
		m[entry.ID] = entry
	}
	return m
}

// PricePoint is what one name cost at one snapshot. ID is what the name
// resolved to there: an alias can be repointed at a new generation, and
// then the jump in the series is a different model, not a price cut.
type PricePoint struct {
	Version string
	ID      string
	In, Out float64
}

// Series follows an id or alias through the snapshots, oldest first,
// skipping the ones taken before the entry existed.
func Series(snaps []Snapshot, name string) []PricePoint {
	var points []PricePoint
	for _, s := range snaps {
		m := s.Catalog.ByName(name)
		if m == nil {
			continue
		}
		points = append(points, PricePoint{
			Version: s.Catalog.Version, ID: m.ID,
			In: m.InputPerMtok, Out: m.OutputPerMtok,
		})
	}
	return points
}
