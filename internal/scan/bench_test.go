package scan

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/MithrilBytes/overwater/catalog"
)

const benchFiles = 1000

// BenchmarkAnalyze measures full pipeline throughput over a synthetic
// repo of clean call sites. CI runs it on every push so a speed
// regression shows up next to the change that caused it.
func BenchmarkAnalyze(b *testing.B) {
	dir := b.TempDir()
	src := filepath.Join(dir, "src")
	if err := os.MkdirAll(src, 0o755); err != nil {
		b.Fatal(err)
	}
	content := `const client = new (require("openai"))();

async function handler(text) {
  return client.chat.completions.create({
    model: "gpt-5-mini",
    temperature: 0,
    max_tokens: 200,
    messages: [{ role: "user", content: text }],
  });
}
`
	for i := 0; i < benchFiles; i++ {
		if err := os.WriteFile(filepath.Join(src, fmt.Sprintf("h%d.js", i)), []byte(content), 0o644); err != nil {
			b.Fatal(err)
		}
	}
	cat, err := catalog.Embedded()
	if err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		report, err := Analyze(dir, cat)
		if err != nil {
			b.Fatal(err)
		}
		if len(report.Sites) != benchFiles {
			b.Fatalf("got %d sites, want %d", len(report.Sites), benchFiles)
		}
	}
	b.ReportMetric(float64(benchFiles*b.N)/b.Elapsed().Seconds(), "files/s")
}
