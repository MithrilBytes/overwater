package catalog

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadDirMatchesEmbeddedSnapshot(t *testing.T) {
	c, err := LoadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	got, err := c.JSON()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, EmbeddedJSON()) {
		t.Fatal("catalog.json is stale; run: go run ./cmd/overwater catalog build")
	}
}

func TestEmbeddedBreadth(t *testing.T) {
	c, err := Embedded()
	if err != nil {
		t.Fatal(err)
	}
	if n := len(c.Models); n < 20 {
		t.Fatalf("catalog has %d models, want at least the original 20 seeds", n)
	}
	providers := map[string]bool{}
	tiers := map[string]bool{}
	deprecated := 0
	for _, m := range c.Models {
		providers[m.Provider] = true
		tiers[m.Tier] = true
		if m.Deprecated != "" {
			deprecated++
		}
	}
	if len(providers) < 5 {
		t.Errorf("catalog spans %d providers, want at least 5", len(providers))
	}
	for _, tier := range Tiers {
		if !tiers[tier] {
			t.Errorf("seed catalog has no %s tier entry", tier)
		}
	}
	if deprecated == 0 {
		t.Error("catalog needs at least one deprecated entry for the deprecated-model rule")
	}
}

func TestNamesResolveAliases(t *testing.T) {
	c, err := Embedded()
	if err != nil {
		t.Fatal(err)
	}
	m := c.ByName("claude-haiku-4-5-20251001")
	if m == nil || m.ID != "claude-haiku-4-5" {
		t.Fatalf("alias lookup returned %+v, want claude-haiku-4-5", m)
	}
	if c.ByName("gpt-9000") != nil {
		t.Fatal("unknown name resolved to an entry")
	}
}

func validModel() Model {
	return Model{
		ID:            "test-model",
		Provider:      "testco",
		InputPerMtok:  1,
		OutputPerMtok: 2,
		ContextWindow: 1000,
		Tier:          "mid",
		Released:      "2025-01-01",
		Source:        "https://example.com/pricing",
	}
}

func TestValidateRejectsBadEntries(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(*Model)
		wantErr string
	}{
		{"missing id", func(m *Model) { m.ID = "" }, "missing an id"},
		{"missing provider", func(m *Model) { m.Provider = "" }, "provider is required"},
		{"zero input price", func(m *Model) { m.InputPerMtok = 0 }, "input_per_mtok"},
		{"negative output price", func(m *Model) { m.OutputPerMtok = -1 }, "output_per_mtok"},
		{"zero context window", func(m *Model) { m.ContextWindow = 0 }, "context_window"},
		{"bad tier", func(m *Model) { m.Tier = "huge" }, "tier"},
		{"bad release date", func(m *Model) { m.Released = "January 2025" }, "released"},
		{"bad deprecation date", func(m *Model) { m.Deprecated = "soon" }, "deprecated"},
		{"non https source", func(m *Model) { m.Source = "http://example.com" }, "source"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			m := validModel()
			tt.mutate(&m)
			c := &Catalog{Version: "2026-01-01", Models: []Model{m}}
			err := c.Validate()
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("Validate() = %v, want error containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestValidateRejectsDuplicateNames(t *testing.T) {
	a := validModel()
	b := validModel()
	b.ID = "other-model"
	b.Aliases = []string{"test-model"}
	c := &Catalog{Version: "2026-01-01", Models: []Model{a, b}}
	if err := c.Validate(); err == nil || !strings.Contains(err.Error(), "appears in both") {
		t.Fatalf("Validate() = %v, want duplicate name error", err)
	}
}

func TestValidateRejectsBadVersion(t *testing.T) {
	c := &Catalog{Version: "latest", Models: []Model{validModel()}}
	if err := c.Validate(); err == nil || !strings.Contains(err.Error(), "version") {
		t.Fatalf("Validate() = %v, want version error", err)
	}
}

func TestLoadDirRejectsFilenameMismatch(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "models"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "VERSION"), []byte("2026-01-01\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	entry := "id: right-name\nprovider: testco\ninput_per_mtok: 1\noutput_per_mtok: 1\ncontext_window: 10\ntier: mid\nreleased: \"2025-01-01\"\nsource: https://example.com\n"
	if err := os.WriteFile(filepath.Join(dir, "models", "wrong-name.yaml"), []byte(entry), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadDir(dir); err == nil || !strings.Contains(err.Error(), "file name") {
		t.Fatalf("LoadDir() = %v, want filename mismatch error", err)
	}
}

func TestLoadDirRejectsUnknownFields(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "models"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "VERSION"), []byte("2026-01-01\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	entry := "id: m\nprovider: testco\ninput_per_mtok: 1\noutput_per_mtok: 1\ncontext_window: 10\ntier: mid\nreleased: \"2025-01-01\"\nsource: https://example.com\nprice: 4\n"
	if err := os.WriteFile(filepath.Join(dir, "models", "m.yaml"), []byte(entry), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadDir(dir); err == nil {
		t.Fatal("LoadDir accepted an entry with an unknown field")
	}
}
