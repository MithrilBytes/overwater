package scan

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

func TestZZDebugNonExtent(t *testing.T) {
	cases := loadCorpus(t)
	label := map[string]corpusCase{}
	for _, c := range cases {
		label[c.File] = c
	}
	root := filepath.Join("..", "..", "corpus", "testdata")
	files, err := walk(root)
	if err != nil {
		t.Fatal(err)
	}
	a := newAnalyzer(files)
	names := mustCatalog(t).Names()
	for _, f := range files {
		c, ok := label[f.path]
		if !ok {
			continue
		}
		sites, _, _ := a.analyzeFile(f, names)
		for _, s := range sites {
			if !s.Known {
				continue
			}
			r := a.regionFor(s.File, s.Line, s.Col)
			if r.isExtent {
				continue
			}
			content := a.byPath[s.File]
			window := region{start: max(0, r.hit-fileWindowBytes), end: min(len(content), r.hit+fileWindowBytes), hit: r.hit}
			narrow := a.evidenceFor(s.File, s.Shape, r)
			wide := a.evidenceFor(s.File, s.Shape, window)
			var added []string
			for _, sig := range endpointSignals {
				if strings.Contains(wide.markers, sig.marker) && !strings.Contains(narrow.markers, sig.marker) {
					added = append(added, sig.marker+"->"+sig.archetype)
				}
			}
			for _, m := range []string{"tool_call_id", "tool_result", "tool_use_id", "function_response", "functionresponse", "toolresult"} {
				if strings.Contains(wide.markers, m) {
					added = append(added, "WIDE:"+m)
				}
				if strings.Contains(narrow.markers, m) {
					added = append(added, "NARROW:"+m)
				}
			}
			fmt.Printf("%-8s %-15s %-40s got %-15s %-6s narrow %d..%d wide %d..%d added %v\n",
				c.Split, c.Archetype, c.File, s.Archetype, s.ArchetypeConfidence, r.start, r.end, window.start, window.end, added)
		}
	}
}
