package rules

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/MithrilBytes/overwater/catalog"
	"github.com/MithrilBytes/overwater/internal/scan"
)

// analyzeRepo scans a repository written from a file map, so the fan in
// tests see the call graph the scanner really builds rather than a
// hand set field.
func analyzeRepo(t *testing.T, cat *catalog.Catalog, files map[string]string) *scan.Report {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		full := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	report, err := scan.Analyze(dir, cat)
	if err != nil {
		t.Fatal(err)
	}
	return report
}

// wrapper returns a call site inside a helper that count places call,
// all of them taking its default model.
func wrapper(ref string, count int) scan.Site {
	s := site(ref, scan.ArchetypeExtraction, scan.Shape{})
	s.FanIn, s.FanInStatus, s.FanInFunc = count, scan.FanInExact, "complete"
	s.CallerModels = []scan.CallerModel{{Ref: ref, ModelID: ref, Known: true, Count: count}}
	return s
}

// The helper everyone calls is priced for everyone who calls it.
func TestFanInMultipliesUniformCallers(t *testing.T) {
	engine, cat := loadEngine(t)
	s := wrapper("claude-opus-5", 4)
	got := engine.Evaluate(&scan.Report{Sites: []scan.Site{s}}, cat)
	if len(got) != 1 {
		t.Fatalf("got %+v, want one finding", got)
	}
	if got[0].Volume != 40000 || got[0].VolumeSource != VolumeFanIn || got[0].Callers != 4 {
		t.Errorf("volume = %d from %s across %d callers, want 40000 fan-in across 4",
			got[0].Volume, got[0].VolumeSource, got[0].Callers)
	}
	// Four times the calls is four times the money.
	leaf := site("claude-opus-5", scan.ArchetypeExtraction, scan.Shape{})
	base := engine.Evaluate(&scan.Report{Sites: []scan.Site{leaf}}, cat)
	if got[0].MonthlyUSD != base[0].MonthlyUSD*4 {
		t.Errorf("monthly = %d, want %d", got[0].MonthlyUSD, base[0].MonthlyUSD*4)
	}
}

// The case the whole layer exists for: a helper with the model written
// inside it, called from two hundred places, is two hundred call sites
// worth of traffic.
func TestFanInPricesAHelperWithNoModelParameter(t *testing.T) {
	engine, cat := loadEngine(t)
	s := site("claude-opus-5", scan.ArchetypeExtraction, scan.Shape{})
	s.FanIn, s.FanInStatus, s.FanInFunc = 200, scan.FanInExact, "complete"
	got := engine.Evaluate(&scan.Report{Sites: []scan.Site{s}}, cat)
	if len(got) != 1 || got[0].Volume != 2000000 || got[0].Callers != 200 {
		t.Fatalf("got %+v, want 2,000,000 calls across 200 callers", got)
	}
}

// A count nobody could establish is not a count. Only an exact
// resolution multiplies, whatever number rides along with it.
func TestFanInOnlyMultipliesExactResolutions(t *testing.T) {
	engine, cat := loadEngine(t)
	for _, status := range []string{scan.FanInDirect, scan.FanInAmbiguous, scan.FanInUnresolved} {
		t.Run(status, func(t *testing.T) {
			s := wrapper("claude-opus-5", 8)
			s.FanInStatus = status
			got := engine.Evaluate(&scan.Report{Sites: []scan.Site{s}}, cat)
			if len(got) != 1 {
				t.Fatalf("got %+v, want one finding", got)
			}
			if got[0].Volume != 10000 || got[0].VolumeSource != VolumeEstimate || got[0].Callers != 0 {
				t.Errorf("volume = %d from %s across %d callers, want the 10000 estimate",
					got[0].Volume, got[0].VolumeSource, got[0].Callers)
			}
		})
	}
}

// The wrapper the trap is about: a default model and eight callers,
// three of which pass a model of their own.
const mixedWrapper = `import Anthropic from "@anthropic-ai/sdk";

const client = new Anthropic();

export async function complete(prompt, model = "claude-opus-5") {
  return client.messages.create({
    model,
    max_tokens: 1024,
    messages: [{ role: "user", content: prompt }],
  });
}
`

// The three callers that pass their own model are call sites already.
// The wrapper answers for the five that take its default, so the repo
// is priced for the eight calls it makes and not for eleven.
func TestFanInMixedCallersAreNotBilledTwice(t *testing.T) {
	engine, cat := loadEngine(t)
	files := map[string]string{"llm.js": mixedWrapper}
	for _, name := range []string{"cheap0", "cheap1", "cheap2"} {
		files[name+".js"] = "async function " + name + "(t) {\n  return complete(t, \"claude-haiku-4-5\");\n}\n"
	}
	for _, name := range []string{"plain0", "plain1", "plain2", "plain3", "plain4"} {
		files[name+".js"] = "async function " + name + "(t) {\n  return complete(t);\n}\n"
	}
	report := analyzeRepo(t, cat, files)
	if len(report.Sites) != 4 {
		t.Fatalf("got %d sites, want the wrapper and the three literal callers: %+v",
			len(report.Sites), report.Sites)
	}

	// Hand computed: the wrapper carries the five calls that take its
	// default, and each caller naming claude-haiku-4-5 carries its own.
	calls := map[string]int{"llm.js": 5, "cheap0.js": 1, "cheap1.js": 1, "cheap2.js": 1}
	names := cat.Names()
	var want float64
	for _, s := range report.Sites {
		if n := engine.callers(s); n != calls[s.File] {
			t.Errorf("%s is priced for %d callers, want %d", s.File, n, calls[s.File])
		}
		want += engine.monthlyUSD(names[s.Ref], s, calls[s.File]*engine.Est.Volume.CallsPerMonth)
	}
	if got := round(engine.TotalMonthlyUSD(report, cat)); got != round(want) {
		t.Errorf("repo total = %d, want %d: eight calls a month, not eleven", got, round(want))
	}
}

// Measured traffic is this call site's own count, fan in included, so
// multiplying it again would bill the callers twice.
func TestMeasuredVolumeBeatsFanIn(t *testing.T) {
	engine, cat := loadEngine(t)
	s := wrapper("claude-opus-5", 4)
	s.File, s.Line = "llm.js", 5
	engine.Volumes = mustParseVolumes(t, `{"sites": {"llm.js:5": 50000}}`)
	got := engine.Evaluate(&scan.Report{Sites: []scan.Site{s}}, cat)
	if len(got) != 1 || got[0].Volume != 50000 || got[0].VolumeSource != VolumeMeasured || got[0].Callers != 0 {
		t.Fatalf("got %+v, want the measured 50000 unmultiplied", got)
	}
}

// A volume pragma is a statement about this call site, not about one
// of its callers.
func TestPragmaBeatsFanIn(t *testing.T) {
	engine, cat := loadEngine(t)
	s := wrapper("claude-opus-5", 4)
	s.VolumeOverride = 999
	got := engine.Evaluate(&scan.Report{Sites: []scan.Site{s}}, cat)
	if len(got) != 1 || got[0].Volume != 999 || got[0].VolumeSource != VolumePragma || got[0].Callers != 0 {
		t.Fatalf("got %+v, want the pragma's 999 unmultiplied", got)
	}
}

// --volume and a config volume are per call site assumptions, and a
// helper's callers are call sites, so fan in multiplies them.
func TestFanInMultipliesTheFlagVolume(t *testing.T) {
	engine, cat := loadEngine(t)
	engine.DefaultVolumeSource = VolumeFlag
	engine.Est.Volume.CallsPerMonth = 2000
	got := engine.Evaluate(&scan.Report{Sites: []scan.Site{wrapper("claude-opus-5", 4)}}, cat)
	if len(got) != 1 || got[0].Volume != 8000 || got[0].VolumeSource != VolumeFanIn {
		t.Fatalf("got %+v, want 8000 from four callers of the flag volume", got)
	}
}

// No fixture centralizes its calls, so no fixture gains a multiplier
// and the five goldens hold still.
func TestFixturesHaveNoFanInMultiplier(t *testing.T) {
	engine, cat := loadEngine(t)
	for _, name := range []string{
		"ts-chat-firehose", "py-extraction", "node-cron-summarizer",
		"rag-frontier-embeddings", "clean-app",
	} {
		report, err := scan.Analyze(filepath.Join("..", "fixtures", name), cat)
		if err != nil {
			t.Fatal(err)
		}
		for _, s := range report.Sites {
			if n := engine.callers(s); n != 1 {
				t.Errorf("%s %s:%d is priced for %d callers, want 1", name, s.File, s.Line, n)
			}
		}
	}
}
