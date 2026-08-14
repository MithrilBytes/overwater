// Package baseline implements the ratchet: the first run records what
// already exists, and only findings absent from that record can fail a
// build. Matching is by fingerprint of the call site itself, not line
// number, so code drifting around a file does not churn the baseline,
// and a file that only moved keeps the entries it had.
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

// version is the current on disk format. Version 3 added the per entry
// site hash, which is what lets a renamed file keep its entries; version
// 2 added the Recorded date; version 1 files still load with every entry
// undated.
const version = 3

const dateFormat = "2006-01-02"

// Entry is one baselined finding. The fingerprint is the key; rule,
// file, and model are there to make the diff readable.
type Entry struct {
	Fingerprint string `json:"fingerprint"`
	Rule        string `json:"rule"`
	File        string `json:"file"`
	Model       string `json:"model"`
	// Recorded is the YYYY-MM-DD day the entry was written, empty when
	// the file predates version 2.
	Recorded string `json:"recorded,omitempty"`
	// Site is the call site's own content hash, the half of the
	// fingerprint no rename touches. Empty in files written before
	// version 3, and such an entry only ever matches at the path it was
	// recorded under.
	Site string `json:"site,omitempty"`
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

// Fingerprint identifies a finding at a path across line drift: rule id
// plus the repo relative path plus a hash of the call site's own text.
// Entries are written under it and matching prefers it; a site that
// moved matches on siteKey instead.
func Fingerprint(f rules.Finding) string {
	h := sha256.Sum256([]byte(f.RuleID + "\x00" + f.File + "\x00" + f.SiteHash))
	return hex.EncodeToString(h[:])[:16]
}

// siteKey is the path free half of the fingerprint, what a finding and
// its entry still share after a git mv.
func siteKey(rule, site string) string { return rule + "\x00" + site }

// matchAll pairs each finding with the baseline entry that absorbs it,
// -1 where nothing does. Exact fingerprints are claimed first, so a
// site that stayed put keeps its own entry and only the leftovers fall
// back to the path free key. Matching stays multiset either way: two
// identical call sites need two entries, so copying one into a second
// file still reads as new while moving it does not.
//
// scanned is the set of files a partial scan covered, nil for a full
// one. It gates the fallback: after a full scan an entry left unclaimed
// is one whose call site is gone, but a partial scan never looked at
// most files, and letting a copy claim the entry of a file it did not
// read would launder a new call site as a move.
func matchAll(findings []rules.Finding, bl *File, scanned map[string]bool) []int {
	byPrint, bySite := map[string][]int{}, map[string][]int{}
	for i, e := range bl.Findings {
		byPrint[e.Fingerprint] = append(byPrint[e.Fingerprint], i)
		// Version 2 entries carry no site hash, and a partial scan cannot
		// vouch for a file it never read; both stay fingerprint only.
		if e.Site != "" && (scanned == nil || scanned[e.File]) {
			k := siteKey(e.Rule, e.Site)
			bySite[k] = append(bySite[k], i)
		}
	}
	claimed := make([]bool, len(bl.Findings))
	claim := func(pool map[string][]int, key string) int {
		for len(pool[key]) > 0 {
			i := pool[key][0]
			pool[key] = pool[key][1:]
			if !claimed[i] {
				claimed[i] = true
				return i
			}
		}
		return -1
	}
	out := make([]int, len(findings))
	for i, f := range findings {
		out[i] = claim(byPrint, Fingerprint(f))
	}
	for i, f := range findings {
		if out[i] < 0 {
			out[i] = claim(bySite, siteKey(f.RuleID, f.SiteHash))
		}
	}
	return out
}

// Entries converts findings into entries stamped with today's date.
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
			Site:        f.SiteHash,
		})
	}
	return entries
}

// Outside returns the entries recorded for files absent from scanned,
// keeping their original dates, so a partial scan cannot prune what it
// never looked at.
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
// prune anything fixed since the last record. Commit is the scanned
// root's git HEAD, empty outside a repository.
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

// Load reads and validates a baseline. Errors here are exit 2, never
// findings.
func Load(path string) (*File, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read baseline: %w (run once with --update-baseline to record one)", err)
	}
	var f File
	if err := json.Unmarshal(raw, &f); err != nil {
		return nil, fmt.Errorf("baseline %s is not valid JSON: %w", path, err)
	}
	if f.Version < 1 || f.Version > version {
		return nil, fmt.Errorf("baseline %s has version %d, this build understands 1 through %d", path, f.Version, version)
	}
	return &f, nil
}

// Aged is a matched baseline entry recorded longer ago than the limit.
// Days is -1 when the recorded date does not parse: an unreadable date
// counts as always aged, never as fresh.
type Aged struct {
	Entry Entry
	Days  int
}

// AgedMatches returns the baseline entries that absorb a finding and
// were recorded more than maxDays days before now. Undated version 1
// entries never age. Matching is the same pass NewFindings makes, so an
// entry nags at most once per run and a moved file nags where it now
// lives.
func AgedMatches(findings []rules.Finding, bl *File, scanned map[string]bool, now time.Time, maxDays int) []Aged {
	if maxDays <= 0 {
		return nil
	}
	var out []Aged
	for _, m := range matchAll(findings, bl, scanned) {
		if m < 0 {
			continue
		}
		e := bl.Findings[m]
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

// NewFindings returns the findings no baseline entry absorbs. scanned
// is the set of files a partial scan covered, nil for a full one.
func NewFindings(findings []rules.Finding, bl *File, scanned map[string]bool) []rules.Finding {
	var out []rules.Finding
	for i, m := range matchAll(findings, bl, scanned) {
		if m < 0 {
			out = append(out, findings[i])
		}
	}
	return out
}
