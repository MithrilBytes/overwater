// Package scan implements the detection layers: manifests, model strings,
// and call site shape. It reads files and emits typed signals; deciding
// what counts as wasteful belongs to the rules engine.
package scan

import (
	"fmt"
	"runtime"
	"sort"
	"sync"

	"github.com/MithrilBytes/overwater/catalog"
)

// SDK is one LLM dependency found in a manifest (layer 1).
type SDK struct {
	Ecosystem string // npm, pypi, gomod, rubygems
	Name      string
	File      string
}

// Shape is what layer 3 could read around a call site. Pointer fields
// distinguish absent from zero, because absence is a signal in its own
// right. BatchContext and BatchAPI are file scoped: a cron trigger and
// the call it drives rarely sit on adjacent lines.
type Shape struct {
	Readable          bool
	Temperature       *float64
	MaxTokens         *int
	MaxRetries        *int
	Dimensions        *int
	Effort            string
	ImageDetailHigh   bool
	JSONSchema        bool
	SchemaEnumOnly    bool
	SchemaMultiField  bool
	Tools             bool
	ForcedTool        bool
	Streaming         bool
	SystemPromptChars int
	SystemPromptText  string
	CacheControl      bool
	EmbeddingCall     bool
	BatchContext      bool
	BatchAPI          bool
}

// Site is one model reference in the scanned repo, with everything the
// layers could see about it. Unknown model looking strings are reported
// with Known false rather than dropped.
type Site struct {
	File                string // slash separated, relative to the repo root
	Line                int
	Col                 int    // byte offset of the reference in its line
	Ref                 string // the string as written in the source
	ModelID             string // catalog id, empty when unknown
	Known               bool
	Archetype           string // filled by the classifier, layer 4
	ArchetypeConfidence string // high, medium, or low; pragmas pin high
	Hash                string // content hash of the call site, stable across line drift
	Ignored             bool   // overwater:ignore pragma
	VolumeOverride      int    // overwater:volume pragma, calls per month
	ViaConfig           string // set when the model arrived via config tracing
	NearbyStrings       []string
	Shape               Shape
}

// Report is the scanner's output for one repository.
type Report struct {
	Root  string
	SDKs  []SDK
	Sites []Site
}

// Analyze runs layers 1 through 3 over the repository at root.
func Analyze(root string, cat *catalog.Catalog) (*Report, error) {
	return AnalyzeOnly(root, cat, nil)
}

// analyzeFile is the per file pipeline: model refs, shape, hash,
// archetype, pragmas, nearby strings. The analyzer already holds the
// full file set for cross file resolution.
func (a *analyzer) analyzeFile(f file, names map[string]*catalog.Model) []Site {
	var sites []Site
	for _, site := range findModelRefs(f.path, f.data, names) {
		regionStart, regionEnd, extStart, hasExtent := a.regionFor(f.path, site.Line, site.Col)
		site.Shape = a.extractShape(f.path, regionStart, regionEnd, extStart, hasExtent)
		site.Hash = a.siteHash(f.path, site.Line, regionStart, regionEnd, hasExtent)
		tier := ""
		if site.Known {
			tier = names[site.Ref].Tier
		}
		hit := a.hitOffsetIn(f.path, site.Line, site.Col)
		site.Archetype, site.ArchetypeConfidence = a.classify(f.path, site.Shape, regionStart, regionEnd, hit, tier)
		site.Ignored, site.VolumeOverride = a.pragmas(f.path, regionStart, regionEnd)
		site.NearbyStrings = a.nearbyStrings(f.path, regionStart, regionEnd)
		sites = append(sites, site)
	}
	return sites
}

// AnalyzeOnly is Analyze restricted to the files named in only (slash
// separated, root relative); nil scans everything the walker visits.
// Incremental callers pass the set git reports changed. The whole repo
// still loads as analyzer context either way: import hop prompt
// resolution, tsconfig aliases, and config tracing must see the same
// files a full scan sees, or the ratchet would compare findings
// produced under different resolution power. The only set restricts
// which files produce output, not which files inform it.
func AnalyzeOnly(root string, cat *catalog.Catalog, only map[string]bool) (*Report, error) {
	files, err := walk(root)
	if err != nil {
		return nil, fmt.Errorf("scan %s: %w", root, err)
	}
	report := &Report{Root: root}
	names := cat.Names()
	a := newAnalyzer(files)

	// Files are independent until the merge, so analyze them across the
	// machine's cores. Results land in walk order slots, keeping output
	// deterministic regardless of which worker finishes first.
	type fileResult struct {
		sdks  []SDK
		sites []Site
	}
	results := make([]fileResult, len(files))
	workers := min(runtime.GOMAXPROCS(0), len(files))
	if workers < 1 {
		workers = 1
	}
	work := make(chan int)
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range work {
				f := files[i]
				if only != nil && !only[f.path] {
					continue // context only, no output
				}
				r := &results[i]
				r.sdks = scanManifest(f.path, f.data)
				r.sites = a.analyzeFile(f, names)
			}
		}()
	}
	for i := range files {
		work <- i
	}
	close(work)
	wg.Wait()
	for _, r := range results {
		report.SDKs = append(report.SDKs, r.sdks...)
		report.Sites = append(report.Sites, r.sites...)
	}
	a.traceConfigModels(report, names, only)
	// Total order: Col separates two models on one line, Ref breaks the
	// residual tie, so equal sites can never swap between runs.
	sort.Slice(report.Sites, func(i, j int) bool {
		a, b := report.Sites[i], report.Sites[j]
		if a.File != b.File {
			return a.File < b.File
		}
		if a.Line != b.Line {
			return a.Line < b.Line
		}
		if a.Col != b.Col {
			return a.Col < b.Col
		}
		return a.Ref < b.Ref
	})
	return report, nil
}
