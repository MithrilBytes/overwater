package scan

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
)

// analyzer carries the walked files so shape extraction and prompt
// resolution can follow one import hop inside the scanned repo, and
// caches the masked view of each file.
type analyzer struct {
	byPath map[string]string
	paths  []string // sorted byPath keys; candidate walks stay deterministic
	// mu guards the lazy caches below; byPath is complete before any
	// worker starts and is then read only.
	mu        sync.Mutex
	maskCache map[string]maskedFile
	spanCache map[string][]span
	lineCache map[string][]int
	factCache map[string]fileFacts
	// factsRuns counts fileFacts evaluations so a test can prove the file
	// scoped regexes run per file, not per site. An atomic add on a path
	// that already runs two regexes costs nothing in production.
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
	// Computed outside the lock; a rare duplicate computation when two
	// workers cross into the same file beats serializing all masking.
	m = maskFile(p, a.byPath[p])
	a.mu.Lock()
	a.maskCache[p] = m
	a.mu.Unlock()
	return m
}

// factsFor returns the file scoped shape facts, computing them at most
// once per file in the common case.
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

// spansFor caches the comment and string spans of a file.
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

// lineStartsFor caches the line index of a file. Rebuilding it per call
// site is a full pass over the file each time, which is the same
// quadratic trap as the file wide regexes.
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

// siteHash fingerprints the call site's own text so the baseline
// ratchet survives line drift. Extent sites hash the prose masked
// extent with whitespace collapsed: moving the call or editing prompt
// prose changes nothing, changing the call's parameters does. Fallback
// sites hash their own line only.
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
