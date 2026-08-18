package catalog

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEmbeddedMatchesLoadDir(t *testing.T) {
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

func TestValidateRejectsEntries(t *testing.T) {
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

func TestHasCapability(t *testing.T) {
	m := validModel()
	m.Capabilities = []string{"vision", "dimensions"}
	if !m.HasCapability("vision") || !m.HasCapability("dimensions") {
		t.Error("declared capabilities not reported")
	}
	if m.HasCapability("tools") || m.HasCapability("") {
		t.Error("undeclared capability reported")
	}
	bare := validModel()
	if bare.HasCapability("vision") {
		t.Error("model with no capability list reported one")
	}
}

// A model that bills a hidden reasoning chain declares it, so pricing
// can tell it from one costed on its visible answer alone.
func TestReasoningEntriesDeclareIt(t *testing.T) {
	c, err := Embedded()
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{
		"o3", "o3-mini", "o4-mini",
		"gpt-5", "gpt-5.1", "gpt-5-mini", "gpt-5-nano",
		"gemini-3-pro", "deepseek-reasoner", "magistral-medium-2506",
	} {
		m := c.ByName(id)
		if m == nil {
			t.Errorf("%s is not in the catalog", id)
			continue
		}
		if !m.HasCapability("reasoning") {
			t.Errorf("%s declares %v, want reasoning among them", id, m.Capabilities)
		}
	}
}

// An entry with no capability list is indistinguishable from one that
// supports nothing, so a capability gated rule drops its call sites
// without saying so. Embedding entries are exempt: the only capability
// they would carry is dimensions, which the shape reader cannot yet
// read at every provider's spelling.
func TestActiveEntriesDeclareCapabilities(t *testing.T) {
	c, err := Embedded()
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range c.Models {
		if m.Deprecated != "" || m.Tier == "embedding" {
			continue
		}
		if len(m.Capabilities) == 0 {
			t.Errorf("%s is active and declares no capabilities", m.ID)
		}
	}
}

// Nomination treats an undated entry as a live candidate, so a retired
// id missing its date is advice to migrate onto a model the provider
// has already switched off.
func TestRetiredEntriesCarryTheirDate(t *testing.T) {
	c, err := Embedded()
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{
		"grok-3", "grok-3-mini", "kimi-k2", "mistral-large-2411",
		"magistral-medium-2506", "gemini-2.0-flash", "gemini-2.0-flash-lite",
		"gemini-3-pro", "llama-3.1-8b-instant", "llama-3.3-70b-versatile",
	} {
		m := c.ByName(id)
		if m == nil {
			t.Errorf("%s is not in the catalog", id)
			continue
		}
		if m.Deprecated == "" {
			t.Errorf("%s is retired upstream but carries no deprecation date", id)
		}
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

func TestValidateRejectsBadRosterDate(t *testing.T) {
	c := &Catalog{Version: "2026-01-01", RosterVerified: "recently", Models: []Model{validModel()}}
	if err := c.Validate(); err == nil || !strings.Contains(err.Error(), "roster_verified") {
		t.Fatalf("Validate() = %v, want roster date error", err)
	}
}

// The roster date is read from its own file so it can lag VERSION, and
// a source directory without one still loads.
func TestLoadDirRosterDate(t *testing.T) {
	write := func(t *testing.T, roster string) string {
		t.Helper()
		dir := t.TempDir()
		if err := os.MkdirAll(filepath.Join(dir, "models"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "VERSION"), []byte("2026-01-01\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if roster != "" {
			if err := os.WriteFile(filepath.Join(dir, "ROSTER"), []byte(roster+"\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		entry := "id: m\nprovider: testco\ninput_per_mtok: 1\noutput_per_mtok: 1\ncontext_window: 10\ntier: mid\nreleased: \"2025-01-01\"\nsource: https://example.com\n"
		if err := os.WriteFile(filepath.Join(dir, "models", "m.yaml"), []byte(entry), 0o644); err != nil {
			t.Fatal(err)
		}
		return dir
	}

	t.Run("dated roster", func(t *testing.T) {
		c, err := LoadDir(write(t, "2025-03-04"))
		if err != nil {
			t.Fatal(err)
		}
		if c.RosterVerified != "2025-03-04" {
			t.Errorf("RosterVerified = %q, want the date in ROSTER", c.RosterVerified)
		}
	})
	t.Run("no roster file", func(t *testing.T) {
		c, err := LoadDir(write(t, ""))
		if err != nil {
			t.Fatal(err)
		}
		if c.RosterVerified != "" {
			t.Errorf("RosterVerified = %q, want it empty", c.RosterVerified)
		}
	})
}

// The shipped catalog dates its model list, or nothing measures how far
// behind the providers it has fallen.
func TestShippedCatalogDatesItsRoster(t *testing.T) {
	c, err := Embedded()
	if err != nil {
		t.Fatal(err)
	}
	if c.RosterVerified == "" {
		t.Fatal("the shipped catalog carries no roster date")
	}
}

func TestLoadDirFilenameMismatch(t *testing.T) {
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

func TestLoadDirUnknownFields(t *testing.T) {
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
