package cli

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

const configName = ".overwater.yaml"

// repoConfig is everything a repo may pin about its own scan, from an
// .overwater.yaml at the scan root. Volume loses to an explicit
// --volume; thresholds override numeric When fields on the named rules;
// exceeding budget_monthly_usd is a findings failure; exclude drops
// findings by path.
type repoConfig struct {
	Volume           int                           `yaml:"volume"`
	BudgetMonthlyUSD float64                       `yaml:"budget_monthly_usd"`
	Disable          []string                      `yaml:"disable"`
	Exclude          []string                      `yaml:"exclude"`
	Thresholds       map[string]map[string]float64 `yaml:"thresholds"`
}

// excluded reports whether a root relative, slash separated path is
// covered by one of the exclude globs. A pattern without a slash
// matches any one path segment, so "*.json" reaches a registry at any
// depth and "reports" covers a directory wherever it sits; a pattern
// with a slash matches the whole path, or names a directory and covers
// the tree under it.
//
// disable is the only other lever and it is repo wide, so a data file
// that merely names model ids costs the rule everywhere. A generated
// file, a vendored fixture and this tool's own reports cannot carry an
// overwater:ignore pragma either, which leaves the repo nothing to say
// about them without this.
func (c *repoConfig) excluded(file string) bool {
	if c == nil {
		return false
	}
	for _, pat := range c.Exclude {
		pat = strings.TrimSuffix(pat, "/")
		if !strings.Contains(pat, "/") {
			for _, seg := range strings.Split(file, "/") {
				if ok, _ := path.Match(pat, seg); ok {
					return true
				}
			}
			continue
		}
		if ok, _ := path.Match(pat, file); ok {
			return true
		}
		if strings.HasPrefix(file, pat+"/") {
			return true
		}
	}
	return false
}

// loadRepoConfig reads root/.overwater.yaml; a missing file is not an
// error. The decode is strict, so a typo exits 2 rather than scanning
// unconfigured.
func loadRepoConfig(root string) (*repoConfig, error) {
	raw, err := os.ReadFile(filepath.Join(root, configName))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	dec := yaml.NewDecoder(bytes.NewReader(raw))
	dec.KnownFields(true)
	var cfg repoConfig
	if err := dec.Decode(&cfg); err != nil {
		if errors.Is(err, io.EOF) {
			return &cfg, nil // an empty config configures nothing
		}
		return nil, fmt.Errorf("%s: %w", configName, err)
	}
	if cfg.Volume < 0 || cfg.BudgetMonthlyUSD < 0 {
		return nil, fmt.Errorf("%s: volume and budget_monthly_usd must not be negative", configName)
	}
	// A pattern that matches nothing silently is worse than no pattern:
	// the repo believes it is excluded and the findings keep coming.
	for _, pat := range cfg.Exclude {
		if pat == "" {
			return nil, fmt.Errorf("%s: exclude has an empty pattern", configName)
		}
		if strings.Contains(pat, "**") {
			return nil, fmt.Errorf("%s: exclude pattern %q: ** is not special here; a pattern with no slash already matches at any depth", configName, pat)
		}
		if _, err := path.Match(strings.TrimSuffix(pat, "/"), ""); err != nil {
			return nil, fmt.Errorf("%s: exclude pattern %q: %w", configName, pat, err)
		}
	}
	return &cfg, nil
}
