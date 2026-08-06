package scan

import (
	"testing"

	"github.com/MithrilBytes/overwater/catalog"
)

func testNames(t *testing.T) map[string]*catalog.Model {
	t.Helper()
	return mustCatalog(t).Names()
}

func TestFindModelRefsResolvesAliases(t *testing.T) {
	content := []byte(`model = "claude-haiku-4-5-20251001"` + "\n")
	sites := findModelRefs("app.py", content, testNames(t))
	if len(sites) != 1 {
		t.Fatalf("got %d sites, want 1: %+v", len(sites), sites)
	}
	if sites[0].ModelID != "claude-haiku-4-5" || sites[0].Ref != "claude-haiku-4-5-20251001" {
		t.Errorf("site = %+v, want the dated alias resolved to its id", sites[0])
	}
}

func TestFindModelRefsRespectsBoundaries(t *testing.T) {
	// claude-opus-55 must not read as claude-opus-5, but it still looks
	// like a model string, so it is reported as unknown.
	content := []byte(`model = "claude-opus-55"` + "\n")
	sites := findModelRefs("app.py", content, testNames(t))
	if len(sites) != 1 {
		t.Fatalf("got %d sites, want 1: %+v", len(sites), sites)
	}
	if sites[0].Known || sites[0].Ref != "claude-opus-55" {
		t.Errorf("site = %+v, want an unknown reference", sites[0])
	}
}

func TestFindModelRefsReportsUnknownModels(t *testing.T) {
	content := []byte(`model: "gpt-6-preview"` + "\n")
	sites := findModelRefs("app.ts", content, testNames(t))
	if len(sites) != 1 {
		t.Fatalf("got %d sites, want 1: %+v", len(sites), sites)
	}
	if sites[0].Known || sites[0].Ref != "gpt-6-preview" {
		t.Errorf("site = %+v, want unknown gpt-6-preview", sites[0])
	}
}

func TestExtractShapeUnreadableConfig(t *testing.T) {
	a := newAnalyzer([]file{{path: ".env", data: []byte("MODEL=gpt-5.1\n")}})
	regionStart, regionEnd, extStart, hasExtent := a.regionFor(".env", 1, 6)
	if hasExtent {
		t.Fatal("a bare env assignment should not produce a call extent")
	}
	shape := a.extractShape(".env", regionStart, regionEnd, extStart, hasExtent)
	if shape.Readable {
		t.Errorf("shape = %+v, want unreadable for a bare env assignment", shape)
	}
	arch, conf := a.classify(".env", shape, regionStart, regionEnd, 6, "")
	if arch != ArchetypeUnknown || conf != "low" {
		t.Errorf("archetype = %s %s, want unknown at low confidence", arch, conf)
	}
}

func TestScanManifestNpm(t *testing.T) {
	content := []byte(`{"dependencies": {"openai": "^4.0.0", "left-pad": "1.0.0"}, "devDependencies": {"ai": "^4.0.0"}}`)
	sdks := scanManifest("package.json", content)
	if len(sdks) != 2 {
		t.Fatalf("got %+v, want openai and ai", sdks)
	}
}

func TestScanManifestRequirements(t *testing.T) {
	content := []byte("# deps\nanthropic>=0.40\nnumpy==1.26.0\n")
	sdks := scanManifest("requirements.txt", content)
	if len(sdks) != 1 || sdks[0].Name != "anthropic" || sdks[0].Ecosystem != "pypi" {
		t.Fatalf("got %+v, want anthropic", sdks)
	}
}

func TestScanManifestGoMod(t *testing.T) {
	content := []byte("module example.com/app\n\nrequire github.com/anthropics/anthropic-sdk-go v1.0.0\n")
	sdks := scanManifest("go.mod", content)
	if len(sdks) != 1 || sdks[0].Ecosystem != "gomod" {
		t.Fatalf("got %+v, want the anthropic Go SDK", sdks)
	}
}

func TestScanManifestIgnoresOtherFiles(t *testing.T) {
	if sdks := scanManifest("main.py", []byte("import openai\n")); sdks != nil {
		t.Fatalf("got %+v, want nil for a non manifest file", sdks)
	}
}
