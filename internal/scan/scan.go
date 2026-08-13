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
// distinguish absent from zero: absence is a signal of its own.
// BatchContext and BatchAPI are file scoped, since a cron trigger and
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
	// FanIn is how many call sites reach this one through the function
	// that holds it, and is never below 1. FanInStatus says whether
	// that number was counted or is only a floor, so a wrapper nobody
	// visibly calls is not priced as a leaf by accident (fanin.go).
	FanIn       int
	FanInFunc   string // the enclosing function, empty at file scope
	FanInStatus string // direct, exact, ambiguous, or unresolved
	// CallerModels lists the models callers pass in where this site is
	// a wrapper's default model. Counts sum to at most FanIn: a caller
	// whose argument could not be read is left out.
	CallerModels []CallerModel
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

// analyzeFile runs layers 2 through 4 over one file.
func (a *analyzer) analyzeFile(f file, names map[string]*catalog.Model) []Site {
	var sites []Site
	// Documentation and tests name models without calling them
	// (emit.go). They stay loaded as context for prompts, constants and
	// fan in; they just do not report sites of their own.
	if !emitsSites(f.path) {
		return nil
	}
	// The code view, not the raw bytes: a model named in a comment is
	// somebody writing about a call, not making one, and it used to
	// become a call site of its own, with a price and its own findings.
	// Strings stay whole because a model id can sit inside a long one,
	// and blanking preserves offsets so line and column still hold.
	for _, site := range findModelRefs(f.path, []byte(a.masked(f.path).code), names) {
		tier := ""
		if site.Known {
			tier = names[site.Ref].Tier
		}
		a.describe(&site, tier)
		sites = append(sites, site)
	}
	// In a config file a model bound to a key is a call site the program
	// reads; a model in a list is one option among many and nothing
	// calls it.
	if isConfigPath(f.path) {
		sites = keepConfigBindings(a.byPath[f.path], sites)
	}
	return sites
}

// AnalyzeOnly is Analyze restricted to the files named in only (slash
// separated, root relative); nil scans everything the walker visits.
// The whole repo loads as context either way. only restricts which
// files produce sites, not which files inform them, so an incremental
// scan resolves prompts and aliases exactly as a full scan does and the
// ratchet compares like with like.
func AnalyzeOnly(root string, cat *catalog.Catalog, only map[string]bool) (*Report, error) {
	files, err := walk(root)
	if err != nil {
		return nil, fmt.Errorf("scan %s: %w", root, err)
	}
	report := &Report{Root: root}
	names := cat.Names()
	a := newAnalyzer(files)

	// Files are independent until the merge. Results land in walk order
	// slots, so output does not depend on which worker finishes first.
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
	a.applyFanIn(report, names)
	// A total order: two models on one line differ by Col, then by Ref,
	// so equal sites can never swap between runs.
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
