package cli

import (
	"os"
	"testing"
)

// TestMain isolates the catalog cache: tests never touch the
// developer's real one, and its prices never change results.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "overwater-test-cache")
	if err == nil {
		os.Setenv("OVERWATER_CACHE_DIR", dir)
		defer os.RemoveAll(dir)
	}
	os.Exit(m.Run())
}
