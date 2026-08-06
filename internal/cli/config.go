// Per repo configuration: an .overwater.yaml at the scan root. The
// decode is strict; an unknown field is an operational error, exit 2,
// so a typo fails loudly instead of silently scanning unconfigured.
package cli

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

const configName = ".overwater.yaml"

// repoConfig is everything a repo may pin about its own scan. Volume
// loses to an explicit --volume flag; thresholds override numeric When
// fields on the named rules; budget_monthly_usd turns the total
// estimated spend into a findings failure when exceeded.
type repoConfig struct {
	Volume           int                           `yaml:"volume"`
	BudgetMonthlyUSD float64                       `yaml:"budget_monthly_usd"`
	Disable          []string                      `yaml:"disable"`
	Thresholds       map[string]map[string]float64 `yaml:"thresholds"`
}

// loadRepoConfig reads root/.overwater.yaml. A missing file is not an
// error; a malformed or unknown field is.
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
	return &cfg, nil
}
