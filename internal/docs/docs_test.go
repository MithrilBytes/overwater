package docs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func root() string { return filepath.Join("..", "..") }

func repoFile(t *testing.T, parts ...string) string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(append([]string{root()}, parts...)...))
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

func facts(t *testing.T) Facts {
	t.Helper()
	f, err := Read(root())
	if err != nil {
		t.Fatal(err)
	}
	return f
}

// The committed pages must be what the generator produces. On main the
// docs workflow rewrites them, so this is what tells a pull request the
// same thing before it merges.
func TestDocsAreInSync(t *testing.T) {
	f := facts(t)
	for _, tc := range []struct {
		path  string
		apply func(string, Facts) (string, error)
	}{
		{"README.md", README},
		{filepath.Join("site", "index.html"), Site},
	} {
		src := repoFile(t, strings.Split(tc.path, string(filepath.Separator))...)
		out, err := tc.apply(src, f)
		if err != nil {
			t.Fatalf("%s: %v", tc.path, err)
		}
		if out != src {
			t.Errorf("%s is out of sync with the repository; run\n"+
				"  go run ./tools/sync-docs", tc.path)
		}
	}
}

// A number inside a release note records what shipped that day. "v2.0
// added twelve rules and 88 catalog entries" is still true and must
// survive a generator that knows there are fifteen rules now. This is
// the mistake the generator exists to avoid making, so it is pinned.
func TestReleaseHistoryIsNotRewritten(t *testing.T) {
	src := repoFile(t, "README.md")
	const history = "twelve rules"
	if !strings.Contains(src, history) {
		t.Skip("the v2.0 release note no longer says twelve rules")
	}
	out, err := README(src, facts(t))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, history) {
		t.Error("the generator rewrote a count inside a release note")
	}
	if !strings.Contains(out, "88 catalog entries") {
		t.Error("the generator rewrote the catalog count inside a release note")
	}
}

func TestReadFindsTheRepositorysOwnFacts(t *testing.T) {
	f := facts(t)
	if !strings.HasPrefix(f.Version, "v") {
		t.Errorf("version = %q, want a vX.Y.Z from action.yml", f.Version)
	}
	if len(f.Rules) < 10 {
		t.Errorf("found %d rules, want the shipped set", len(f.Rules))
	}
	for i := 1; i < len(f.Rules); i++ {
		if f.Rules[i-1] >= f.Rules[i] {
			t.Fatalf("rules are not sorted: %v", f.Rules)
		}
	}
	if f.CatalogModels < 50 || f.CatalogVenders < 5 {
		t.Errorf("catalog = %d models over %d providers, want the shipped catalog",
			f.CatalogModels, f.CatalogVenders)
	}
	if strings.Contains(strings.Join(f.Rules, " "), "estimates") {
		t.Error("estimates.yaml was counted as a rule")
	}
}

// A rule added without touching the README is the drift that started
// this: the list read as authoritative and was three rules behind.
func TestGeneratedListCarriesEveryRule(t *testing.T) {
	f := facts(t)
	out, err := README(repoFile(t, "README.md"), f)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range f.Rules {
		if !strings.Contains(out, "`"+id+"`") {
			t.Errorf("generated README does not list %s", id)
		}
	}
	f.Rules = append(f.Rules, "brand-new-rule")
	out, err = README(repoFile(t, "README.md"), f)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "`brand-new-rule`") {
		t.Error("a new rule did not reach the generated list")
	}
}

func TestWrapKeepsLinesUnderTheColumn(t *testing.T) {
	long := []string{
		"uncapped-embedding-dimensions", "hot-temperature-extraction",
		"thinking-budget-overkill", "uncached-system-prompt", "batch-on-realtime",
	}
	for _, line := range strings.Split(wrap(long, 72), "\n") {
		if len(line) > 72 {
			t.Errorf("line is %d columns: %q", len(line), line)
		}
	}
	if got := wrap([]string{"only-one"}, 72); got != "`only-one`." {
		t.Errorf("single id = %q", got)
	}
}
