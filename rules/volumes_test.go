package rules

import (
	"reflect"
	"strings"
	"testing"

	"github.com/MithrilBytes/overwater/internal/scan"
)

func mustParseVolumes(t *testing.T, raw string) *Volumes {
	t.Helper()
	v, err := ParseVolumes([]byte(raw))
	if err != nil {
		t.Fatalf("ParseVolumes(%s) = %v", raw, err)
	}
	return v
}

// A site key prices that call site off real traffic and says so.
func TestMeasuredSiteVolume(t *testing.T) {
	engine, cat := loadEngine(t)
	s := site("claude-opus-5", scan.ArchetypeExtraction, scan.Shape{})
	s.File, s.Line = "extract.py", 24
	base := engine.Evaluate(&scan.Report{Sites: []scan.Site{s}}, cat)
	if len(base) != 1 || base[0].VolumeSource != VolumeEstimate || base[0].Volume != 10000 {
		t.Fatalf("without a volumes file: %+v", base)
	}
	engine.Volumes = mustParseVolumes(t, `{"sites": {"extract.py:24": 50000}}`)
	got := engine.Evaluate(&scan.Report{Sites: []scan.Site{s}}, cat)
	if len(got) != 1 {
		t.Fatalf("got %+v, want one finding", got)
	}
	if got[0].Volume != 50000 || got[0].VolumeSource != VolumeMeasured {
		t.Errorf("volume = %d from %s, want 50000 measured", got[0].Volume, got[0].VolumeSource)
	}
	// Five times the calls is five times the money.
	if got[0].MonthlyUSD != base[0].MonthlyUSD*5 {
		t.Errorf("monthly = %d, want %d", got[0].MonthlyUSD, base[0].MonthlyUSD*5)
	}
}

// A model key covers every site on that model, split evenly.
func TestModelVolumeSplitsAcrossSites(t *testing.T) {
	engine, cat := loadEngine(t)
	a := site("claude-opus-5", scan.ArchetypeExtraction, scan.Shape{})
	b := site("claude-opus-5", scan.ArchetypeExtraction, scan.Shape{})
	a.File, a.Line = "one.py", 3
	b.File, b.Line = "two.py", 9
	engine.Volumes = mustParseVolumes(t, `{"models": {"claude-opus-5": 60000}}`)
	got := engine.Evaluate(&scan.Report{Sites: []scan.Site{a, b}}, cat)
	if len(got) != 2 {
		t.Fatalf("got %+v, want two findings", got)
	}
	for _, f := range got {
		if f.Volume != 30000 || f.VolumeSource != VolumeMeasured {
			t.Errorf("%s: volume = %d from %s, want 30000 measured", f.File, f.Volume, f.VolumeSource)
		}
	}
}

// The more specific key wins, and it does not change what the other
// sites on that model split.
func TestSiteKeyBeatsModelKey(t *testing.T) {
	engine, cat := loadEngine(t)
	a := site("claude-opus-5", scan.ArchetypeExtraction, scan.Shape{})
	b := site("claude-opus-5", scan.ArchetypeExtraction, scan.Shape{})
	a.File, a.Line = "one.py", 3
	b.File, b.Line = "two.py", 9
	engine.Volumes = mustParseVolumes(t,
		`{"sites": {"one.py:3": 7}, "models": {"claude-opus-5": 60000}}`)
	got := engine.Evaluate(&scan.Report{Sites: []scan.Site{a, b}}, cat)
	if len(got) != 2 {
		t.Fatalf("got %+v, want two findings", got)
	}
	if got[0].Volume != 7 {
		t.Errorf("one.py volume = %d, want the site key's 7", got[0].Volume)
	}
	if got[1].Volume != 30000 {
		t.Errorf("two.py volume = %d, want its share of the model key", got[1].Volume)
	}
}

// Measured traffic beats a volume pragma.
func TestMeasuredBeatsPragma(t *testing.T) {
	engine, cat := loadEngine(t)
	s := site("claude-opus-5", scan.ArchetypeExtraction, scan.Shape{})
	s.File, s.Line = "extract.py", 24
	s.VolumeOverride = 999
	engine.Volumes = mustParseVolumes(t, `{"sites": {"extract.py:24": 50000}}`)
	got := engine.Evaluate(&scan.Report{Sites: []scan.Site{s}}, cat)
	if len(got) != 1 || got[0].Volume != 50000 || got[0].VolumeSource != VolumeMeasured {
		t.Fatalf("got %+v, want the measured 50000", got)
	}
	// With no key for that site the pragma is back in charge.
	engine.Volumes = mustParseVolumes(t, `{"sites": {"other.py:1": 50000}}`)
	got = engine.Evaluate(&scan.Report{Sites: []scan.Site{s}}, cat)
	if len(got) != 1 || got[0].Volume != 999 || got[0].VolumeSource != VolumePragma {
		t.Fatalf("got %+v, want the pragma's 999", got)
	}
}

// A file the scan cannot place is named, not swallowed.
func TestUnmatchedVolumeKeys(t *testing.T) {
	engine, cat := loadEngine(t)
	s := site("claude-opus-5", scan.ArchetypeExtraction, scan.Shape{})
	s.File, s.Line = "extract.py", 24
	engine.Volumes = mustParseVolumes(t,
		`{"sites": {"extract.py:24": 5, "moved.py:1": 5}, "models": {"gpt-4o": 5}}`)
	report := &scan.Report{Sites: []scan.Site{s}}
	want := []string{"no call site at moved.py:1", "no call site uses model gpt-4o"}
	if got := engine.UnmatchedVolumeKeys(report, cat); !reflect.DeepEqual(got, want) {
		t.Errorf("unmatched = %v, want %v", got, want)
	}
}

// The budget check and the printed report price the same call sites the
// same way, or CI fails over a number nobody can see.
func TestTotalMonthlyUSDUsesMeasuredVolumes(t *testing.T) {
	engine, cat := loadEngine(t)
	a := site("claude-opus-5", scan.ArchetypeExtraction, scan.Shape{})
	b := site("claude-opus-5", scan.ArchetypeExtraction, scan.Shape{})
	a.File, a.Line = "one.py", 3
	b.File, b.Line = "two.py", 4
	engine.Volumes = mustParseVolumes(t, `{"sites": {"one.py:3": 20000, "two.py:4": 5000}}`)
	report := &scan.Report{Sites: []scan.Site{a, b}}
	m := cat.ByName("claude-opus-5")
	want := round(engine.monthlyUSD(m, a, 20000) + engine.monthlyUSD(m, b, 5000))
	if total := round(engine.TotalMonthlyUSD(report, cat)); total != want {
		t.Errorf("total = %d, want %d", total, want)
	}
	sum := 0
	for _, f := range engine.Evaluate(report, cat) {
		sum += f.MonthlyUSD
	}
	if sum != want {
		t.Errorf("findings sum to %d, want the %d the budget check saw", sum, want)
	}
}

// An ignored site draws no finding but still spends, so it still takes
// a share of its model's measured traffic.
func TestIgnoredSiteTakesItsShare(t *testing.T) {
	engine, cat := loadEngine(t)
	a := site("claude-opus-5", scan.ArchetypeExtraction, scan.Shape{})
	b := site("claude-opus-5", scan.ArchetypeExtraction, scan.Shape{})
	a.File, b.File = "one.py", "two.py"
	b.Ignored = true
	engine.Volumes = mustParseVolumes(t, `{"models": {"claude-opus-5": 60000}}`)
	got := engine.Evaluate(&scan.Report{Sites: []scan.Site{a, b}}, cat)
	if len(got) != 1 || got[0].Volume != 30000 {
		t.Fatalf("got %+v, want one finding at 30000", got)
	}
}

// An alias in the file lands on the sites that spell the model that way.
func TestModelKeyMatchesAlias(t *testing.T) {
	engine, cat := loadEngine(t)
	m := cat.ByName("claude-opus-5")
	if m == nil || len(m.Aliases) == 0 {
		t.Skip("claude-opus-5 carries no alias in this catalog")
	}
	s := site(m.Aliases[0], scan.ArchetypeExtraction, scan.Shape{})
	engine.Volumes = mustParseVolumes(t, `{"models": {"`+m.Aliases[0]+`": 40000}}`)
	got := engine.Evaluate(&scan.Report{Sites: []scan.Site{s}}, cat)
	if len(got) != 1 || got[0].Volume != 40000 || got[0].VolumeSource != VolumeMeasured {
		t.Fatalf("got %+v, want the alias key to apply", got)
	}
}

func TestParseVolumesRejects(t *testing.T) {
	cases := []struct {
		name    string
		raw     string
		wantErr string
	}{
		{"not json", "calls: 5\n", "invalid character"},
		{"unknown field", `{"site": {"a.ts:1": 5}}`, "unknown field"},
		{"site key without a line", `{"sites": {"a.ts": 5}}`, "not file:line"},
		{"site key with a word for a line", `{"sites": {"a.ts:top": 5}}`, "not file:line"},
		{"negative site count", `{"sites": {"a.ts:1": -5}}`, "negative"},
		{"negative model count", `{"models": {"gpt-4o": -1}}`, "negative"},
		{"measures nothing", `{}`, "no sites and no models"},
		{"trailing junk", `{"models": {"gpt-4o": 1}} {}`, "trailing data"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseVolumes([]byte(tc.raw))
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("ParseVolumes = %v, want an error containing %q", err, tc.wantErr)
			}
		})
	}
}

func TestParseVolumesRoundTrips(t *testing.T) {
	v := mustParseVolumes(t, `{"sites": {"a.ts:1": 5}, "models": {"gpt-4o": 9}}`)
	raw, err := v.JSON()
	if err != nil {
		t.Fatal(err)
	}
	back, err := ParseVolumes(raw)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(v, back) {
		t.Errorf("round trip = %+v, want %+v", back, v)
	}
}

// The engine falls back to the flag or config volume for the sites the
// file does not name, and labels them with where that number came from.
func TestDefaultVolumeSourceCarries(t *testing.T) {
	engine, cat := loadEngine(t)
	engine.DefaultVolumeSource = VolumeFlag
	engine.Est.Volume.CallsPerMonth = 2000
	s := site("claude-opus-5", scan.ArchetypeExtraction, scan.Shape{})
	got := engine.Evaluate(&scan.Report{Sites: []scan.Site{s}}, cat)
	if len(got) != 1 || got[0].Volume != 2000 || got[0].VolumeSource != VolumeFlag {
		t.Fatalf("got %+v, want 2000 from the flag", got)
	}
}
