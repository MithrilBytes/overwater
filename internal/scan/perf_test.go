package scan

import (
	"fmt"
	"strings"
	"testing"
)

// minifiedRegistry builds one line of JSON holding n model references,
// the shape a generated model registry or a minified config takes.
func minifiedRegistry(n int) string {
	var b strings.Builder
	b.WriteString(`{"models":{`)
	for i := 0; i < n; i++ {
		if i > 0 {
			b.WriteByte(',')
		}
		fmt.Fprintf(&b, `"entry_%d":{"model":"gpt-4o-mini","temperature":0.2,"max_tokens":512,"label":"row %d"}`, i, i)
	}
	b.WriteString("}}")
	return b.String()
}

// File scoped facts must be read once per file. Reading them per call
// site is what made a minified config cost minutes: the work is
// identical for every reference in the file.
func TestFileScopedFactsComputeOncePerFile(t *testing.T) {
	files := []file{
		{path: "registry.json", data: []byte(minifiedRegistry(60))},
		{path: "cron.js", data: []byte("const cron = require(\"node-cron\");\ncron.schedule(\"0 * * * *\", () => call(\"gpt-5-mini\"));\n")},
	}
	a := newAnalyzer(files)
	names := mustCatalog(t).Names()
	sites := 0
	for _, f := range files {
		sites += len(a.analyzeFile(f, names))
	}
	if sites < 60 {
		t.Fatalf("got %d sites, want at least 60", sites)
	}
	if got := a.factsRuns.Load(); got != int64(len(files)) {
		t.Errorf("file scoped facts computed %d times over %d files and %d sites; want once per file",
			got, len(files), sites)
	}
}

// No call site may read a region that grows with the file. A minified
// file is one enormous line, so the line based bounds degenerate: both
// the extent path (registry.json, whose objects name a model) and the
// fallback window path (ids.json, a bare list with no model key and no
// closer within reach) used to hand every reference a region as long as
// its own offset, which is quadratic in references per file.
func TestRegionsStayBoundedInMinifiedFiles(t *testing.T) {
	var ids strings.Builder
	ids.WriteString(`{"ids":[`)
	for i := 0; i < 4000; i++ {
		if i > 0 {
			ids.WriteByte(',')
		}
		ids.WriteString(`"gpt-4o-mini"`)
	}
	ids.WriteString("]}")

	// Spelled as a literal rather than from the constants above, so that
	// widening a bound cannot quietly widen the test with it. Generous:
	// the head expansion and the fallback window both sit well under it,
	// while the unbounded version reached the whole file.
	const bound = 24000
	names := mustCatalog(t).Names()
	for _, tc := range []struct{ path, content string }{
		{"registry.json", minifiedRegistry(400)},
		{"ids.json", ids.String()},
	} {
		a := newAnalyzer([]file{{path: tc.path, data: []byte(tc.content)}})
		refs := findModelRefs(tc.path, []byte(tc.content), names)
		if len(refs) < 400 {
			t.Fatalf("%s: got %d references, want at least 400", tc.path, len(refs))
		}
		for _, r := range refs {
			start, end, _, _ := a.regionFor(tc.path, r.Line, r.Col)
			if end-start > bound {
				t.Fatalf("%s: reference at %d:%d got a %d byte region in a %d byte file; bound is %d",
					tc.path, r.Line, r.Col, end-start, len(tc.content), bound)
			}
		}
	}
}
