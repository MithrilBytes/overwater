package cli

import (
	"os"
	"testing"
)

// TestMain isolates the catalog cache so tests never read or write the
// developer's real cache, and a dev machine's newer cached prices can
// never change test results.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "overwater-test-cache")
	if err == nil {
		os.Setenv("OVERWATER_CACHE_DIR", dir)
		defer os.RemoveAll(dir)
	}
	os.Exit(m.Run())
}
