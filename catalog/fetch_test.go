package catalog

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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

// A cache of {"version":"9999-01-01","models":[]} used to win on the
// version string alone and silently empty the detection dictionary.
// Validate now rejects the empty model list, so Effective ignores the
// cache, says so, and keeps the embedded snapshot.
func TestEffectiveRejectsEmptyFutureCache(t *testing.T) {
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

// A valid cache that outranks the embedded snapshot must announce the
// swap, naming both versions, so the cli can surface it.
func TestEffectiveNotesWhenCacheShadowsEmbedded(t *testing.T) {
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
