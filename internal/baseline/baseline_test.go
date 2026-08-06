package baseline

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MithrilBytes/overwater/rules"
)

func finding(rule, file, hash string) rules.Finding {
	return rules.Finding{RuleID: rule, File: file, SiteHash: hash}
}

func TestNewFindingsIsAMultiset(t *testing.T) {
	a := finding("r", "f.ts", "aaaa")
	findings := []rules.Finding{a, a, finding("r", "f.ts", "bbbb")}

	bl := &File{Version: 1, Findings: []Entry{{Fingerprint: Fingerprint(a)}}}
	fresh := NewFindings(findings, bl)
	if len(fresh) != 2 {
		t.Fatalf("got %d new findings, want 2: one duplicate and one distinct", len(fresh))
	}
}

func TestFingerprintIgnoresLineButNotContent(t *testing.T) {
	a := finding("r", "f.ts", "aaaa")
	b := a
	b.Line = 99
	if Fingerprint(a) != Fingerprint(b) {
		t.Error("fingerprint changed with the line number")
	}
	c := a
	c.SiteHash = "cccc"
	if Fingerprint(a) == Fingerprint(c) {
		t.Error("fingerprint ignored the call site content")
	}
}

func TestWriteThenLoadRoundTrips(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bl.json")
	if err := Write(path, []rules.Finding{finding("r", "f.ts", "aaaa")}); err != nil {
		t.Fatal(err)
	}
	bl, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(bl.Findings) != 1 || bl.Findings[0].Rule != "r" {
		t.Fatalf("round trip = %+v", bl.Findings)
	}
}

func TestLoadRejectsWrongVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bl.json")
	if err := os.WriteFile(path, []byte(`{"version": 99, "findings": []}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil || !strings.Contains(err.Error(), "version") {
		t.Fatalf("Load = %v, want a version error", err)
	}
}

func TestLoadAcceptsVersionOneAsUndated(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bl.json")
	v1 := `{"version": 1, "findings": [{"fingerprint": "abcd", "rule": "r", "file": "f.ts", "model": "m"}]}`
	if err := os.WriteFile(path, []byte(v1), 0o644); err != nil {
		t.Fatal(err)
	}
	bl, err := Load(path)
	if err != nil {
		t.Fatalf("Load rejected a version 1 file: %v", err)
	}
	if len(bl.Findings) != 1 || bl.Findings[0].Recorded != "" {
		t.Fatalf("version 1 entries = %+v, want one undated entry", bl.Findings)
	}
}

func TestWriteStampsRecordedToday(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bl.json")
	before := time.Now().Format("2006-01-02")
	if err := Write(path, []rules.Finding{finding("r", "f.ts", "aaaa")}); err != nil {
		t.Fatal(err)
	}
	after := time.Now().Format("2006-01-02")
	bl, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if bl.Version != 2 {
		t.Errorf("version = %d, want 2", bl.Version)
	}
	if got := bl.Findings[0].Recorded; got != before && got != after {
		t.Errorf("recorded = %q, want today (%s)", got, after)
	}
}

func TestAgedMatches(t *testing.T) {
	now := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)
	matched := finding("r", "f.ts", "aaaa")
	entry := func(f rules.Finding, recorded string) Entry {
		return Entry{Fingerprint: Fingerprint(f), Rule: f.RuleID, File: f.File, Recorded: recorded}
	}
	cases := []struct {
		name     string
		findings []rules.Finding
		entries  []Entry
		maxDays  int
		want     int
	}{
		{"aged entry nags", []rules.Finding{matched}, []Entry{entry(matched, "2026-06-01")}, 30, 1},
		{"fresh entry is quiet", []rules.Finding{matched}, []Entry{entry(matched, "2026-08-01")}, 30, 0},
		{"undated version 1 entry never ages", []rules.Finding{matched}, []Entry{entry(matched, "")}, 30, 0},
		{"unmatched entry is quiet", nil, []Entry{entry(matched, "2026-01-01")}, 30, 0},
		{"zero disables aging", []rules.Finding{matched}, []Entry{entry(matched, "2026-01-01")}, 0, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := AgedMatches(tc.findings, &File{Version: version, Findings: tc.entries}, now, tc.maxDays)
			if len(got) != tc.want {
				t.Fatalf("AgedMatches = %d aged entries, want %d", len(got), tc.want)
			}
		})
	}
	aged := AgedMatches([]rules.Finding{matched}, &File{Findings: []Entry{entry(matched, "2026-06-01")}}, now, 30)
	if len(aged) != 1 || aged[0].Days != 66 || aged[0].Entry.File != "f.ts" {
		t.Fatalf("aged = %+v, want one f.ts entry aged 66 days", aged)
	}
}
