package catalog

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// DefaultURL is the published catalog: the committed catalog.json
// served straight from the repository over HTTPS. A static file is the
// entire endpoint; the GitHub Pages mirror deploys the same bytes once
// Pages is enabled on the repo. Fetching this is the only network
// activity the scanner is ever permitted, and even that is optional.
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

// CachePath is where a fetched catalog lives locally. The env override
// exists so tests and odd environments can redirect it.
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

// Effective picks the freshest valid catalog available with zero
// network: the cache when it is newer than the embedded snapshot,
// otherwise the snapshot. A bad cache is reported in note and ignored,
// never fatal. A cache that wins also announces itself in note, naming
// both versions, so a scan never swaps its pricing data silently.
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

// Stale returns a warning when the catalog's price date has aged past
// StaleAfter, empty otherwise. Staleness is worth a sentence, never a
// failure.
func Stale(c *Catalog, now time.Time) string {
	v, err := time.Parse(dateLayout, c.Version)
	if err != nil {
		return ""
	}
	if now.Sub(v) > StaleAfter {
		return fmt.Sprintf("prices are from %s; refresh with: overwater catalog refresh", c.Version)
	}
	return ""
}
