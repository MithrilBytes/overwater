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
	"time"

	"github.com/MithrilBytes/overwater/rules"
)

// version is the current on disk format. Version 2 added the per entry
// Recorded date; version 1 files still load with every entry undated.
const version = 2

const dateFormat = "2006-01-02"

// Entry is one baselined finding. The fingerprint is the key; rule,
// file, and model are there so a human reviewing the diff can tell what
// each line means.
type Entry struct {
	Fingerprint string `json:"fingerprint"`
	Rule        string `json:"rule"`
	File        string `json:"file"`
	Model       string `json:"model"`
	// Recorded is the YYYY-MM-DD day the entry was written, empty when
	// the file predates version 2.
	Recorded string `json:"recorded,omitempty"`
}

// File is the on disk baseline.
type File struct {
	Version int `json:"version"`
	// Commit is the scanned root's git HEAD when the baseline was
	// recorded, empty outside a repository. Incremental scans diff
	// against it.
	Commit   string  `json:"commit,omitempty"`
	Findings []Entry `json:"findings"`
}

// Fingerprint identifies a finding across line drift: rule id plus the
// repo relative path plus a hash of the call site's own text.
func Fingerprint(f rules.Finding) string {
	h := sha256.Sum256([]byte(f.RuleID + "\x00" + f.File + "\x00" + f.SiteHash))
	return hex.EncodeToString(h[:])[:16]
}

// Entries converts findings into baseline entries stamped with today's
// date, so re-recording an entry is an explicit re-acknowledgment.
func Entries(findings []rules.Finding) []Entry {
	today := time.Now().Format(dateFormat)
	entries := make([]Entry, 0, len(findings))
	for _, f := range findings {
		entries = append(entries, Entry{
			Fingerprint: Fingerprint(f),
			Rule:        f.RuleID,
			File:        f.File,
			Model:       f.Model,
			Recorded:    today,
		})
	}
	return entries
}

// Outside returns the entries recorded for files absent from scanned,
// keeping their original dates. An incremental update merges them in so
// a partial scan cannot prune what it never looked at.
func Outside(bl *File, scanned map[string]bool) []Entry {
	var out []Entry
	for _, e := range bl.Findings {
		if !scanned[e.File] {
			out = append(out, e)
		}
	}
	return out
}

// Write records the entries as the new baseline; a full scan's entries
// inherently prune anything fixed since the last record. Commit is the
// scanned root's git HEAD, empty outside a repository.
func Write(path string, entries []Entry, commit string) error {
	sorted := make([]Entry, len(entries))
	copy(sorted, entries)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].Fingerprint != sorted[j].Fingerprint {
			return sorted[i].Fingerprint < sorted[j].Fingerprint
		}
		return sorted[i].Rule < sorted[j].Rule
	})
	b, err := json.MarshalIndent(File{Version: version, Commit: commit, Findings: sorted}, "", "  ")
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
	if f.Version != version && f.Version != 1 {
		return nil, fmt.Errorf("baseline %s has version %d, this build understands 1 and %d", path, f.Version, version)
	}
	return &f, nil
}

// Aged is a matched baseline entry recorded longer ago than the limit.
// Days is -1 when the recorded date does not parse: an unreadable date
// can never age out quietly, so it counts as always aged.
type Aged struct {
	Entry Entry
	Days  int
}

// AgedMatches returns the baseline entries that absorb a finding and
// were recorded more than maxDays days before now. Undated entries
// (version 1 files) never age; entries whose date does not parse come
// back with Days -1. Matching mirrors NewFindings, multiset by
// fingerprint, so an entry nags at most once per run.
func AgedMatches(findings []rules.Finding, bl *File, now time.Time, maxDays int) []Aged {
	if maxDays <= 0 {
		return nil
	}
	pool := map[string][]Entry{}
	for _, e := range bl.Findings {
		pool[e.Fingerprint] = append(pool[e.Fingerprint], e)
	}
	var out []Aged
	for _, f := range findings {
		fp := Fingerprint(f)
		entries := pool[fp]
		if len(entries) == 0 {
			continue
		}
		e := entries[0]
		pool[fp] = entries[1:]
		if e.Recorded == "" {
			continue
		}
		rec, err := time.Parse(dateFormat, e.Recorded)
		if err != nil {
			out = append(out, Aged{Entry: e, Days: -1})
			continue
		}
		if days := int(now.UTC().Sub(rec).Hours() / 24); days > maxDays {
			out = append(out, Aged{Entry: e, Days: days})
		}
	}
	return out
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
