// Package scan implements the detection layers: manifests, model strings,
// and call site shape. It reads files and emits typed signals; deciding
// what counts as wasteful belongs to the rules engine.
package scan

import (
	"fmt"
	"sort"

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
	files, err := walk(root)
	if err != nil {
		return nil, fmt.Errorf("scan %s: %w", root, err)
	}
	report := &Report{Root: root}
	names := cat.Names()
	a := newAnalyzer(files)
	for _, f := range files {
		report.SDKs = append(report.SDKs, scanManifest(f.path, f.data)...)
		for _, site := range findModelRefs(f.path, f.data, names) {
			regionStart, regionEnd, hasExtent := a.regionFor(f.path, site.Line, site.Col)
			site.Shape = a.extractShape(f.path, regionStart, regionEnd)
			site.Hash = a.siteHash(f.path, site.Line, regionStart, regionEnd, hasExtent)
			tier := ""
			if site.Known {
				tier = names[site.Ref].Tier
			}
			hit := hitOffset(string(f.data), site.Line, site.Col)
			site.Archetype, site.ArchetypeConfidence = a.classify(f.path, site.Shape, regionStart, regionEnd, hit, tier)
			report.Sites = append(report.Sites, site)
		}
	}
	sort.Slice(report.Sites, func(i, j int) bool {
		a, b := report.Sites[i], report.Sites[j]
		if a.File != b.File {
			return a.File < b.File
		}
		return a.Line < b.Line
	})
	return report, nil
}
