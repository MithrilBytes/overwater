package catalog

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// cacheDir points the cache at a fresh temp directory and returns it;
// writeCacheFile drops raw bytes there as the cached catalog.
func cacheDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("OVERWATER_CACHE_DIR", dir)
	return dir
}

func writeCacheFile(t *testing.T, dir string, raw []byte) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "catalog.json"), raw, 0o644); err != nil {
		t.Fatal(err)
	}
}

// A cache of {"version":"9999-01-01","models":[]} would win on version
// alone and empty the detection dictionary. Validate rejects the empty
// model list, so Effective ignores the cache and says so.
func TestEffectiveRejectsEmptyCache(t *testing.T) {
	empty := &Catalog{Version: "9999-01-01"}
	if err := empty.Validate(); err == nil || !strings.Contains(err.Error(), "no models") {
		t.Fatalf("Validate() = %v, want an empty model list rejected", err)
	}

	dir := cacheDir(t)
	writeCacheFile(t, dir, []byte(`{"version":"9999-01-01","models":[]}`))
	c, note, err := Effective()
	if err != nil {
		t.Fatal(err)
	}
	emb, err := Embedded()
	if err != nil {
		t.Fatal(err)
	}
	if c.Version != emb.Version || len(c.Models) != len(emb.Models) {
		t.Fatalf("Effective() = catalog %s with %d models, want the embedded snapshot %s", c.Version, len(c.Models), emb.Version)
	}
	if !strings.Contains(note, "ignoring") || !strings.Contains(note, "no models") {
		t.Errorf("note = %q, want the rejected cache called out", note)
	}
}

// Effective's selection across every cache state: the embedded
// snapshot loses only to a valid, strictly newer cache.
func TestEffectiveSelection(t *testing.T) {
	emb, err := Embedded()
	if err != nil {
		t.Fatal(err)
	}
	cacheAt := func(t *testing.T, version string) {
		t.Helper()
		dir := cacheDir(t)
		c := &Catalog{Version: version, Models: []Model{validModel()}}
		raw, err := c.JSON()
		if err != nil {
			t.Fatal(err)
		}
		writeCacheFile(t, dir, raw)
	}
	wantEmbedded := func(t *testing.T, c *Catalog, note, wantNote string) {
		t.Helper()
		if c.Version != emb.Version || len(c.Models) != len(emb.Models) {
			t.Fatalf("Effective() = catalog %s with %d models, want the embedded snapshot", c.Version, len(c.Models))
		}
		if wantNote == "" && note != "" {
			t.Errorf("note = %q, want none", note)
		}
		if wantNote != "" && !strings.Contains(note, wantNote) {
			t.Errorf("note = %q, want it to contain %q", note, wantNote)
		}
	}

	t.Run("no cache", func(t *testing.T) {
		cacheDir(t)
		c, note, err := Effective()
		if err != nil {
			t.Fatal(err)
		}
		wantEmbedded(t, c, note, "")
	})
	t.Run("older cache", func(t *testing.T) {
		cacheAt(t, "2000-01-01")
		c, note, err := Effective()
		if err != nil {
			t.Fatal(err)
		}
		wantEmbedded(t, c, note, "")
	})
	t.Run("newer valid cache", func(t *testing.T) {
		cacheAt(t, "9999-01-01")
		c, note, err := Effective()
		if err != nil {
			t.Fatal(err)
		}
		if c.Version != "9999-01-01" || len(c.Models) != 1 {
			t.Fatalf("Effective() = catalog %s with %d models, want the newer cache", c.Version, len(c.Models))
		}
		if note == "" {
			t.Error("note is empty, want the shadowing announced")
		}
	})
	t.Run("invalid cache", func(t *testing.T) {
		dir := cacheDir(t)
		writeCacheFile(t, dir, []byte("not json"))
		c, note, err := Effective()
		if err != nil {
			t.Fatal(err)
		}
		wantEmbedded(t, c, note, "ignoring")
	})
	t.Run("tie goes to embedded", func(t *testing.T) {
		cacheAt(t, emb.Version)
		c, note, err := Effective()
		if err != nil {
			t.Fatal(err)
		}
		wantEmbedded(t, c, note, "")
	})
}

// Staleness flips strictly after StaleAfter; an unparseable version
// stays silent.
func TestStaleBoundaries(t *testing.T) {
	c := &Catalog{Version: "2026-01-01", Models: []Model{validModel()}}
	dated, err := time.Parse("2006-01-02", c.Version)
	if err != nil {
		t.Fatal(err)
	}
	if warn := Stale(c, dated.Add(StaleAfter)); warn != "" {
		t.Errorf("Stale at exactly the boundary = %q, want none", warn)
	}
	warn := Stale(c, dated.Add(StaleAfter+time.Second))
	if !strings.Contains(warn, c.Version) || !strings.Contains(warn, "refresh") {
		t.Errorf("Stale past the boundary = %q, want the version and refresh hint", warn)
	}
	bad := &Catalog{Version: "not-a-date"}
	if warn := Stale(bad, dated); warn != "" {
		t.Errorf("Stale with a bad version = %q, want silence", warn)
	}
}

func TestCachePathEnvOverride(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("OVERWATER_CACHE_DIR", dir)
	path, err := CachePath()
	if err != nil {
		t.Fatal(err)
	}
	if path != filepath.Join(dir, "catalog.json") {
		t.Errorf("CachePath() = %q, want catalog.json under the override dir", path)
	}
	t.Setenv("OVERWATER_CACHE_DIR", "")
	path, err = CachePath()
	if err != nil {
		t.Skipf("no user cache dir here: %v", err)
	}
	if want := filepath.Join("overwater", "catalog.json"); !strings.HasSuffix(path, want) {
		t.Errorf("CachePath() = %q, want it to end in %q", path, want)
	}
}

func TestWriteCacheRoundTrip(t *testing.T) {
	dir := cacheDir(t)
	path, err := WriteCache(EmbeddedJSON())
	if err != nil {
		t.Fatal(err)
	}
	if path != filepath.Join(dir, "catalog.json") {
		t.Errorf("WriteCache path = %q, want it under the cache dir", path)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != string(EmbeddedJSON()) {
		t.Error("cached bytes differ from what was written")
	}
	cached, err := loadCache()
	if err != nil {
		t.Fatal(err)
	}
	emb, err := Embedded()
	if err != nil {
		t.Fatal(err)
	}
	if cached == nil || cached.Version != emb.Version || len(cached.Models) != len(emb.Models) {
		t.Errorf("loadCache() = %+v, want the embedded snapshot back", cached)
	}
}

// A valid cache that outranks the embedded snapshot must announce the
// swap, naming both versions, so the cli can surface it.
func TestEffectiveNotesCacheWin(t *testing.T) {
	dir := cacheDir(t)
	shadow := &Catalog{Version: "9999-01-01", Models: []Model{validModel()}}
	raw, err := shadow.JSON()
	if err != nil {
		t.Fatal(err)
	}
	writeCacheFile(t, dir, raw)
	c, note, err := Effective()
	if err != nil {
		t.Fatal(err)
	}
	if c.Version != "9999-01-01" || len(c.Models) != 1 {
		t.Fatalf("Effective() = catalog %s with %d models, want the newer cache", c.Version, len(c.Models))
	}
	emb, err := Embedded()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(note, "9999-01-01") || !strings.Contains(note, emb.Version) {
		t.Errorf("note = %q, want both versions named", note)
	}
}
