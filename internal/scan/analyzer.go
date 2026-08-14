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
	lineCache map[string][]int32
	factCache map[string]fileFacts
	// factsRuns counts fileFacts evaluations, so a test can prove the
	// file scoped regexes run per file and not per site.
	factsRuns atomic.Int64
	// The definition and call index, built once per pass and read only
	// afterwards (fanin.go).
	indexOnce sync.Once
	idx       *repoIndex
	// The files that could read an environment variable, built once per
	// pass and read only afterwards (trace.go).
	envOnce  sync.Once
	envCands []envCandidate
	// envCompiles counts reader regexes built, so a test can prove they
	// are built per config key and not per key per file.
	envCompiles atomic.Int64
}

func newAnalyzer(files []file) *analyzer {
	a := &analyzer{
		byPath:    make(map[string]string, len(files)),
		maskCache: map[string]maskedFile{},
		spanCache: map[string][]span{},
		lineCache: map[string][]int32{},
		factCache: map[string]fileFacts{},
	}
	// The walker hands over the contents, it does not lend them: a copy
	// here would leave every file in the repository on the heap twice,
	// once as the walker's string and once as ours, for the whole pass.
	for _, f := range files {
		a.byPath[f.path] = f.data
	}
	a.paths = make([]string, 0, len(a.byPath))
	for p := range a.byPath {
		a.paths = append(a.paths, p)
	}
	sort.Strings(a.paths)
	return a
}

// cached returns cache[p], computing it on a miss. The computation runs
// outside the lock, so two workers entering the same file may both do
// it.
func cached[T any](a *analyzer, cache map[string]T, p string, compute func() T) T {
	a.mu.Lock()
	v, ok := cache[p]
	a.mu.Unlock()
	if ok {
		return v
	}
	v = compute()
	a.mu.Lock()
	cache[p] = v
	a.mu.Unlock()
	return v
}

func (a *analyzer) masked(p string) maskedFile {
	return cached(a, a.maskCache, p, func() maskedFile {
		return maskFile(p, a.byPath[p])
	})
}

// codeView returns layer 2's view of p and hands it to the caller
// rather than caching it. Every view is a full copy of the file, and on
// a monorepo the mask cache is the largest thing the scanner holds;
// this is the one view with a single reader, the model id sweep, which
// is done with it before the next file starts. The same span scan fills
// the cache the other two views live in, so a file masked this way is
// still scanned once.
func (a *analyzer) codeView(p string) string {
	content := a.byPath[p]
	spans := scanSpans(content, familyFor(p))
	views := maskViews(content, spans)
	a.mu.Lock()
	if _, ok := a.maskCache[p]; !ok {
		a.maskCache[p] = views
	}
	a.mu.Unlock()
	return maskCode(content, spans)
}

func (a *analyzer) facts(p string) fileFacts {
	return cached(a, a.factCache, p, func() fileFacts {
		a.factsRuns.Add(1)
		return readFileFacts(a.masked(p).prose)
	})
}

func (a *analyzer) spans(p string) []span {
	return cached(a, a.spanCache, p, func() []span {
		return scanSpans(a.byPath[p], familyFor(p))
	})
}

// lineStarts caches the line index; rebuilding it per call site is a
// full pass over the file each time.
func (a *analyzer) lineStarts(p string) []int32 {
	return cached(a, a.lineCache, p, func() []int32 {
		return lineStarts(a.byPath[p])
	})
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
				end = int(starts[line])
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
	return min(int(starts[line-1])+col, len(a.byPath[p]))
}

// lineStarts indexes the start of every line. int32 offsets, and a
// slice sized to the newline count rather than grown into: one of these
// lives for the whole pass per file touched, and the walker admits
// nothing anywhere near 2GB (maxFileSize).
func lineStarts(s string) []int32 {
	starts := make([]int32, 1, strings.Count(s, "\n")+1)
	for off := 0; ; {
		i := strings.IndexByte(s[off:], '\n')
		if i < 0 {
			return starts
		}
		off += i + 1
		starts = append(starts, int32(off))
	}
}
