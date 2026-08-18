package catalog

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// DefaultURL is the published catalog: the committed catalog.json served
// over HTTPS, with a GitHub Pages mirror of the same bytes. Fetching it
// is the only network call the scanner is permitted, and it is optional.
const DefaultURL = "https://raw.githubusercontent.com/MithrilBytes/overwater/main/catalog/catalog.json"

// StaleAfter is how old prices get before the scanner starts saying so.
const StaleAfter = 90 * 24 * time.Hour

// Fetch downloads and validates a catalog. The raw bytes come back too
// so the caller can cache exactly what was served.
func Fetch(client *http.Client, url string) (*Catalog, []byte, error) {
	resp, err := client.Get(url)
	if err != nil {
		return nil, nil, fmt.Errorf("fetch catalog: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, nil, fmt.Errorf("fetch catalog: %s from %s", resp.Status, url)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, nil, fmt.Errorf("fetch catalog: %w", err)
	}
	var c Catalog
	if err := json.Unmarshal(raw, &c); err != nil {
		return nil, nil, fmt.Errorf("fetched catalog is not valid JSON: %w", err)
	}
	if err := c.Validate(); err != nil {
		return nil, nil, fmt.Errorf("fetched catalog: %w", err)
	}
	return &c, raw, nil
}

// CachePath is where a fetched catalog lives locally. OVERWATER_CACHE_DIR
// redirects it for tests and locked down environments.
func CachePath() (string, error) {
	if dir := os.Getenv("OVERWATER_CACHE_DIR"); dir != "" {
		return filepath.Join(dir, "catalog.json"), nil
	}
	dir, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "overwater", "catalog.json"), nil
}

// WriteCache stores fetched catalog bytes and returns the path.
func WriteCache(raw []byte) (string, error) {
	path, err := CachePath()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	return path, os.WriteFile(path, raw, 0o644)
}

// loadCache returns the cached catalog, nil when there is none, or an
// error when the cache exists but is unusable.
func loadCache() (*Catalog, error) {
	path, err := CachePath()
	if err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var c Catalog
	if err := json.Unmarshal(raw, &c); err != nil {
		return nil, fmt.Errorf("catalog cache %s: %w", path, err)
	}
	if err := c.Validate(); err != nil {
		return nil, fmt.Errorf("catalog cache %s: %w", path, err)
	}
	return &c, nil
}

// Effective picks the freshest valid catalog with no network: the cache
// when it is newer than the embedded snapshot, otherwise the snapshot.
// A bad cache lands in note and is ignored, never fatal. A cache that
// wins names both versions in note; pricing data never swaps silently.
func Effective() (*Catalog, string, error) {
	emb, err := Embedded()
	if err != nil {
		return nil, "", err
	}
	cached, err := loadCache()
	if err != nil {
		return emb, fmt.Sprintf("ignoring %v", err), nil
	}
	if cached != nil && cached.Version > emb.Version {
		note := fmt.Sprintf("using cached catalog %s over embedded snapshot %s", cached.Version, emb.Version)
		return cached, note, nil
	}
	return emb, "", nil
}

// Stale returns a warning when the catalog's price date, or the roster
// date when it carries one, has aged past StaleAfter; empty otherwise.
// The two age apart, so both are checked: a nightly price refresh keeps
// the version current while the model list behind it goes a year
// without a new entry. Never an error: a stale catalog still scans.
func Stale(c *Catalog, now time.Time) string {
	var aged []string
	if agedPast(c.Version, now) {
		aged = append(aged, fmt.Sprintf("prices are from %s", c.Version))
	}
	if agedPast(c.RosterVerified, now) {
		aged = append(aged, fmt.Sprintf("the model list was last checked against the providers %s", c.RosterVerified))
	}
	if len(aged) == 0 {
		return ""
	}
	return strings.Join(aged, "; ") + "; refresh with: overwater catalog refresh"
}

// agedPast reports whether a catalog date is older than StaleAfter. An
// absent or unparseable date says nothing rather than warning.
func agedPast(date string, now time.Time) bool {
	d, err := time.Parse(dateLayout, date)
	if err != nil {
		return false
	}
	return now.Sub(d) > StaleAfter
}
