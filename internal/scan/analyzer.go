package scan

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
)

// analyzer holds every walked file for the whole pass, so resolution can
// follow imports across the repo, and caches the per file views the
// layers keep asking for.
type analyzer struct {
	byPath map[string]string
	paths  []string // sorted byPath keys; candidate walks stay deterministic
	// mu guards the caches below. byPath is complete before any worker
	// starts and is read only from then on.
	mu        sync.Mutex
	maskCache map[string]maskedFile
	spanCache map[string][]span
	lineCache map[string][]int
	factCache map[string]fileFacts
	// factsRuns counts fileFacts evaluations, so a test can prove the
	// file scoped regexes run per file and not per site.
	factsRuns atomic.Int64
}

func newAnalyzer(files []file) *analyzer {
	a := &analyzer{
		byPath:    make(map[string]string, len(files)),
		maskCache: map[string]maskedFile{},
		spanCache: map[string][]span{},
		lineCache: map[string][]int{},
		factCache: map[string]fileFacts{},
	}
	for _, f := range files {
		a.byPath[f.path] = string(f.data)
	}
	a.paths = make([]string, 0, len(a.byPath))
	for p := range a.byPath {
		a.paths = append(a.paths, p)
	}
	sort.Strings(a.paths)
	return a
}

func (a *analyzer) masked(p string) maskedFile {
	a.mu.Lock()
	m, ok := a.maskCache[p]
	a.mu.Unlock()
	if ok {
		return m
	}
	// Masked outside the lock: a rare duplicate when two workers cross
	// into the same file beats serializing all masking.
	m = maskFile(p, a.byPath[p])
	a.mu.Lock()
	a.maskCache[p] = m
	a.mu.Unlock()
	return m
}

func (a *analyzer) facts(p string) fileFacts {
	a.mu.Lock()
	f, ok := a.factCache[p]
	a.mu.Unlock()
	if ok {
		return f
	}
	f = readFileFacts(a.masked(p).prose)
	a.factsRuns.Add(1)
	a.mu.Lock()
	a.factCache[p] = f
	a.mu.Unlock()
	return f
}

func (a *analyzer) spans(p string) []span {
	a.mu.Lock()
	s, ok := a.spanCache[p]
	a.mu.Unlock()
	if ok {
		return s
	}
	s = scanSpans(a.byPath[p], familyFor(p))
	a.mu.Lock()
	a.spanCache[p] = s
	a.mu.Unlock()
	return s
}

// lineStarts caches the line index. Rebuilding it per call site is a
// full pass over the file each time.
func (a *analyzer) lineStarts(p string) []int {
	a.mu.Lock()
	s, ok := a.lineCache[p]
	a.mu.Unlock()
	if ok {
		return s
	}
	s = lineStarts(a.byPath[p])
	a.mu.Lock()
	a.lineCache[p] = s
	a.mu.Unlock()
	return s
}

// describe fills in everything layers 3 and 4 can read about a site
// whose File, Line and Col are already set. tier is the catalog tier of
// the referenced model, empty when the model is unknown.
func (a *analyzer) describe(site *Site, tier string) {
	r := a.regionFor(site.File, site.Line, site.Col)
	site.Shape = a.extractShape(site.File, r)
	site.Hash = a.siteHash(site.File, site.Line, r)
	site.Archetype, site.ArchetypeConfidence = a.classify(site.File, site.Shape, r, tier)
	site.Ignored, site.VolumeOverride = a.pragmas(site.File, r)
	site.NearbyStrings = a.nearbyStrings(site.File, r)
}

// siteHash fingerprints a call site so the baseline ratchet survives
// line drift. An extent site hashes its prose masked extent with
// whitespace collapsed, so moving the call or editing prompt prose
// changes nothing but editing its parameters does; a fallback site
// hashes its own line.
func (a *analyzer) siteHash(p string, line int, r region) string {
	var text string
	if r.isExtent {
		text = a.masked(p).prose[r.start:r.end]
	} else {
		content := a.byPath[p]
		starts := a.lineStarts(p)
		if line-1 < len(starts) {
			end := len(content)
			if line < len(starts) {
				end = starts[line]
			}
			text = content[starts[line-1]:end]
		}
	}
	sum := sha256.Sum256([]byte(strings.Join(strings.Fields(text), " ")))
	return hex.EncodeToString(sum[:])[:16]
}

// hitOffsetIn converts a one based line and column to a byte offset.
func (a *analyzer) hitOffsetIn(p string, line, col int) int {
	starts := a.lineStarts(p)
	if line-1 >= len(starts) {
		return 0
	}
	return min(starts[line-1]+col, len(a.byPath[p]))
}

func lineStarts(s string) []int {
	starts := []int{0}
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			starts = append(starts, i+1)
		}
	}
	return starts
}
