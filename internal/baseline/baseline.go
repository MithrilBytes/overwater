// Package baseline implements the ratchet: the first run records what
// already exists, and only findings absent from that record can fail a
// build. Matching is by fingerprint of the call site itself, not line
// number, so code drifting around a file does not churn the baseline.
package baseline

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"sort"

	"github.com/MithrilBytes/overwater/rules"
)

const version = 1

// Entry is one baselined finding. The fingerprint is the key; rule,
// file, and model are there so a human reviewing the diff can tell what
// each line means.
type Entry struct {
	Fingerprint string `json:"fingerprint"`
	Rule        string `json:"rule"`
	File        string `json:"file"`
	Model       string `json:"model"`
}

// File is the on disk baseline.
type File struct {
	Version  int     `json:"version"`
	Findings []Entry `json:"findings"`
}

// Fingerprint identifies a finding across line drift: rule id plus the
// repo relative path plus a hash of the call site's own text.
func Fingerprint(f rules.Finding) string {
	h := sha256.Sum256([]byte(f.RuleID + "\x00" + f.File + "\x00" + f.SiteHash))
	return hex.EncodeToString(h[:])[:16]
}

// Write records the findings as the new baseline, which inherently
// prunes anything fixed since the last record.
func Write(path string, findings []rules.Finding) error {
	entries := make([]Entry, 0, len(findings))
	for _, f := range findings {
		entries = append(entries, Entry{
			Fingerprint: Fingerprint(f),
			Rule:        f.RuleID,
			File:        f.File,
			Model:       f.Model,
		})
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Fingerprint != entries[j].Fingerprint {
			return entries[i].Fingerprint < entries[j].Fingerprint
		}
		return entries[i].Rule < entries[j].Rule
	})
	b, err := json.MarshalIndent(File{Version: version, Findings: entries}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o644)
}

// Load reads and validates a baseline. Errors here are operational
// (exit 2 territory), never findings.
func Load(path string) (*File, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read baseline: %w (run once with --update-baseline to record one)", err)
	}
	var f File
	if err := json.Unmarshal(raw, &f); err != nil {
		return nil, fmt.Errorf("baseline %s is not valid JSON: %w", path, err)
	}
	if f.Version != version {
		return nil, fmt.Errorf("baseline %s has version %d, this build understands %d", path, f.Version, version)
	}
	return &f, nil
}

// NewFindings returns the findings absent from the baseline. Matching
// is multiset style: two identical call sites need two baseline entries.
func NewFindings(findings []rules.Finding, bl *File) []rules.Finding {
	budget := map[string]int{}
	for _, e := range bl.Findings {
		budget[e.Fingerprint]++
	}
	var out []rules.Finding
	for _, f := range findings {
		fp := Fingerprint(f)
		if budget[fp] > 0 {
			budget[fp]--
			continue
		}
		out = append(out, f)
	}
	return out
}
