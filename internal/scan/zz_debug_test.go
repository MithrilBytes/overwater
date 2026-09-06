package scan

import (
	"os"
	"path/filepath"
	"testing"
)

func TestZZDebugTargets(t *testing.T) {
	for _, name := range []string{"r5_lesson_assistant.rb", "r5_lesson_review.go", "r5_related_courses.rb", "r8_claim_extract.rb"} {
		raw, err := os.ReadFile(filepath.Join("..", "..", "corpus", "testdata", name))
		if err != nil {
			t.Fatal(err)
		}
		r := analyzeTemp(t, map[string]string{name: string(raw)})
		for _, s := range r.Sites {
			t.Logf("%s: %s %s model=%s known=%v fanin=%s", name, s.Archetype, s.ArchetypeConfidence, s.ModelID, s.Known, s.FanInStatus)
		}
	}
}
