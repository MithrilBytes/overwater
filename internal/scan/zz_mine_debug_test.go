package scan

import (
	"path/filepath"
	"sort"
	"testing"
)

func TestZZMineDebug(t *testing.T) {
	root := filepath.Join("..", "..", "corpus", "testdata")
	files, err := walk(root)
	if err != nil {
		t.Fatal(err)
	}
	a := newAnalyzer(files)
	names := mustCatalog(t).Names()
	want := map[string]bool{"r7_digest_strict_group.go": true, "r9_text_quality_gate.py": true}
	for _, f := range files {
		if !want[f.path] {
			continue
		}
		sites, _, _ := a.analyzeFile(f, names)
		for _, s := range sites {
			if !s.Known {
				continue
			}
			r := a.regionFor(s.File, s.Line, s.Col)
			content := a.byPath[s.File]
			t.Logf("== %s got %s %s region %d..%d hit %d extent=%v", s.File, s.Archetype, s.ArchetypeConfidence, r.start, r.end, r.hit, r.isExtent)
			narrow := a.evidenceFor(s.File, s.Shape, r)
			t.Logf("narrow funcName=%q forced=%v image=%v loop=%v", narrow.funcName, narrow.forcedTool, narrow.imageInput, narrow.toolLoop)
			t.Logf("narrow prompt=%q", narrow.prompt)
			t.Logf("narrow markers=%q", narrow.markers)
			dumpScores(t, "narrow", scoreEvidence(s.Shape, narrow))
			window := region{start: max(0, r.hit-fileWindowBytes), end: min(len(content), r.hit+fileWindowBytes), hit: r.hit}
			wide := a.evidenceFor(s.File, s.Shape, window)
			t.Logf("wide funcName=%q forced=%v image=%v loop=%v", wide.funcName, wide.forcedTool, wide.imageInput, wide.toolLoop)
			t.Logf("wide prompt=%q", wide.prompt)
			t.Logf("wide markers=%q", wide.markers)
			dumpScores(t, "wide", scoreEvidence(s.Shape, wide))
		}
	}
}

func dumpScores(t *testing.T, label string, set scoreSet) {
	keys := make([]string, 0, len(set.scores))
	for k := range set.scores {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		t.Logf("  %s %-15s %3d named=%v", label, k, set.scores[k], set.named[k])
	}
}
