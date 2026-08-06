package baseline

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

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
