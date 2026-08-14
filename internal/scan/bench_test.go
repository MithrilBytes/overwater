package scan

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
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

// BenchmarkTraceConfigModels holds the shape of config tracing: many
// config keys over a corpus that mostly cannot read any of them. Cost
// belongs to the keys and the few reader files, so growing the corpus
// alone must not move this number much. It was 83% of a vscode scan when
// each key re-read every file.
func BenchmarkTraceConfigModels(b *testing.B) {
	const (
		traceKeys  = 120
		traceNoise = 300
	)
	var cfg strings.Builder
	for i := 0; i < traceKeys; i++ {
		fmt.Fprintf(&cfg, "SERVICE_%d_MODEL=gpt-5-mini\n", i)
	}
	files := []file{{path: "app.env", data: cfg.String()}}
	for i := 0; i < 4; i++ {
		files = append(files, file{
			path: fmt.Sprintf("src/reader%d.js", i),
			data: fmt.Sprintf("const m = process.env.SERVICE_%d_MODEL;\n", i),
		})
	}
	body := strings.Repeat("// a line of ordinary source that reads no environment variable\n", 30)
	for i := 0; i < traceNoise; i++ {
		files = append(files, file{
			path: fmt.Sprintf("src/n%d.js", i),
			data: fmt.Sprintf("export function n%d(x) { return x + %d; }\n%s", i, i, body),
		})
	}
	cat, err := catalog.Embedded()
	if err != nil {
		b.Fatal(err)
	}
	names := cat.Names()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// A fresh analyzer per iteration: the candidate list is built once
		// per pass, and reusing it would benchmark the cache.
		newAnalyzer(files).traceConfigModels(&Report{}, names, nil)
	}
}
