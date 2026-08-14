package render

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/MithrilBytes/overwater/catalog"
	"github.com/MithrilBytes/overwater/internal/scan"
	"github.com/MithrilBytes/overwater/rules"
)

// The full pipeline run against each fixture must reproduce its golden
// byte for byte. The goldens are the spec: when this fails, the
// renderer or a detector is wrong, not the golden.
func TestScanReproducesGoldens(t *testing.T) {
	fixtures := []string{
		"ts-chat-firehose",
		"py-extraction",
		"node-cron-summarizer",
		"rag-frontier-embeddings",
		"py-agent-pipeline",
		"clean-app",
	}
	cat, err := catalog.Embedded()
	if err != nil {
		t.Fatal(err)
	}
	engine, err := rules.Load()
	if err != nil {
		t.Fatal(err)
	}
	meta := Meta{CatalogVersion: cat.Version, CallsPerMonth: engine.Est.Volume.CallsPerMonth}
	for _, name := range fixtures {
		t.Run(name, func(t *testing.T) {
			report, err := scan.Analyze(filepath.Join("..", "..", "fixtures", name), cat)
			if err != nil {
				t.Fatal(err)
			}
			got := ModelsMD(engine.Evaluate(report, cat), meta)
			want, err := os.ReadFile(filepath.Join("..", "..", "goldens", name+".md"))
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, want) {
				t.Errorf("MODELS.md drifted from the golden\n--- got ---\n%s\n--- want ---\n%s", got, want)
			}
		})
	}
}
