package scan

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
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

// File scoped facts are read once per file. Per call site the work is
// identical for every reference, which costs minutes on a minified
// config.
func TestFileFactsRunOncePerFile(t *testing.T) {
	files := []file{
		{path: "registry.json", data: minifiedRegistry(60)},
		{path: "cron.js", data: "const cron = require(\"node-cron\");\ncron.schedule(\"0 * * * *\", () => call(\"gpt-5-mini\"));\n"},
	}
	a := newAnalyzer(files)
	names := mustCatalog(t).Names()
	sites := 0
	for _, f := range files {
		found, _, _ := a.analyzeFile(f, names)
		sites += len(found)
	}
	if sites < 60 {
		t.Fatalf("got %d sites, want at least 60", sites)
	}
	if got := a.factsRuns.Load(); got != int64(len(files)) {
		t.Errorf("file scoped facts computed %d times over %d files and %d sites; want once per file",
			got, len(files), sites)
	}
}

// Tracing a config key to its readers costs one pass over the corpus for
// the whole scan, not one per key. Files that name no reader syntax are
// dropped once, and the regexes are built per key rather than per key
// per file: on a repo the size of vscode the old shape was thousands of
// corpus walks and millions of compiles, five sixths of the whole scan.
func TestEnvReaderWorkDoesNotScaleWithCorpus(t *testing.T) {
	files := []file{
		{path: "app.env", data: "SUMMARY_MODEL=gpt-5.1\nTRIAGE_MODEL=gpt-5-mini\nDRAFT_MODEL=gpt-5-nano\n"},
		{path: "reader.js", data: `const a = process.env.SUMMARY_MODEL;
const b = process.env.TRIAGE_MODEL;
const c = process.env.DRAFT_MODEL;
`},
	}
	const noise = 40
	for i := 0; i < noise; i++ {
		files = append(files, file{
			path: fmt.Sprintf("noise/n%d.js", i),
			data: fmt.Sprintf("export function n%d(x) { return x + %d; }\n", i, i),
		})
	}
	a := newAnalyzer(files)
	report := &Report{}
	a.traceConfigModels(report, mustCatalog(t).Names(), nil)
	if len(report.Sites) != 3 {
		t.Fatalf("got %d traced sites, want 3: %+v", len(report.Sites), report.Sites)
	}
	if got := len(a.envReaders()); got != 1 {
		t.Errorf("%d of %d files kept as reader candidates; want only the one that names a reader syntax",
			got, len(files))
	}
	// Three keys, and the reader file spells all three the same way, so
	// one pattern each. The bound is generous: per key per file would be
	// over a hundred here and millions on a real repo.
	if got, want := a.envCompiles.Load(), int64(len(envReaderPatterns)); got > want {
		t.Errorf("built %d reader regexes for 3 keys over %d files; want at most %d, one pass per key",
			got, len(files), want)
	}
}

// The analyzer holds the whole repository for the whole pass, so what
// it keeps per walked byte is the memory bill: 16,888 files and 170MB
// of source peaked at 1.16GB of RSS. A ratio, like the scaling gate,
// because an absolute number means nothing across machines.
//
// Two multiples of the repository were being held for nothing. The
// analyzer converted the walker's bytes instead of taking the string it
// was handed, leaving every file resident twice, and the mask cache
// held three views of every masked file when the third has one reader
// and is finished with before the next file starts.
func TestLiveHeapPerWalkedByte(t *testing.T) {
	dir := t.TempDir()
	var src strings.Builder
	for i := 0; i < 1600; i++ {
		fmt.Fprintf(&src, "const label%d = \"row %d of a generated module\"; // note %d\n", i, i, i)
	}
	const fileCount = 80
	for i := 0; i < fileCount; i++ {
		name := filepath.Join(dir, fmt.Sprintf("m%d.ts", i))
		if err := os.WriteFile(name, []byte(src.String()), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	walked := int64(fileCount * src.Len())

	files, err := walk(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != fileCount {
		t.Fatalf("walked %d files, want %d", len(files), fileCount)
	}
	// Two collections: the first frees what the walk allocated, the
	// second the finalizer work the first queued.
	heap := func() int64 {
		runtime.GC()
		runtime.GC()
		var m runtime.MemStats
		runtime.ReadMemStats(&m)
		return int64(m.HeapAlloc)
	}

	base := heap()
	a := newAnalyzer(files)
	for _, f := range files {
		a.masked(f.path)
		a.lineStarts(f.path)
	}
	live := heap() - base
	// The measurement is of what these still reach, so neither may be
	// collected before it is taken.
	runtime.KeepAlive(files)
	runtime.KeepAlive(a)

	// Tenths of the walked bytes. Two views and a line index measure
	// 2.1x here; a third view and a second copy of the file itself
	// measured 4.3x.
	const bound = 26
	tenths := live * 10 / walked
	if tenths > bound {
		t.Errorf("%d files, %d walked bytes, %d bytes live after masking: %d.%dx the input, bound %d.%dx",
			len(files), walked, live, tenths/10, tenths%10, bound/10, bound%10)
	}
}

// No call site may read a region that grows with the file. A minified
// file is one enormous line, so the line based bounds degenerate on both
// paths: the extent path (registry.json, whose objects name a model) and
// the fallback window (ids.json, a bare list with no model key and no
// closer within reach). Unbounded, each is quadratic in references.
func TestRegionsBoundedInMinified(t *testing.T) {
	var ids strings.Builder
	ids.WriteString(`{"ids":[`)
	for i := 0; i < 4000; i++ {
		if i > 0 {
			ids.WriteByte(',')
		}
		ids.WriteString(`"gpt-4o-mini"`)
	}
	ids.WriteString("]}")

	// A literal, not the constants above: widening a bound must not
	// quietly widen the test with it.
	const bound = 24000
	names := mustCatalog(t).Names()
	for _, tc := range []struct{ path, content string }{
		{"registry.json", minifiedRegistry(400)},
		{"ids.json", ids.String()},
	} {
		a := newAnalyzer([]file{{path: tc.path, data: tc.content}})
		refs := findModelRefs(tc.path, tc.content, names)
		if len(refs) < 400 {
			t.Fatalf("%s: got %d references, want at least 400", tc.path, len(refs))
		}
		for _, ref := range refs {
			r := a.regionFor(tc.path, ref.Line, ref.Col)
			if r.end-r.start > bound {
				t.Fatalf("%s: reference at %d:%d got a %d byte region in a %d byte file; bound is %d",
					tc.path, ref.Line, ref.Col, r.end-r.start, len(tc.content), bound)
			}
		}
	}
}

// Every tsconfig used to be re-read and re-unmarshalled for each non
// relative import, so a monorepo paid the parse once per lookup. They
// are parsed once per pass now, the way the index and the env
// candidates already were.
func TestTsconfigsParseOncePerPass(t *testing.T) {
	files := map[string]string{
		"tsconfig.json":              `{"compilerOptions":{"baseUrl":".","paths":{"@lib/*":["src/lib/*"]}}}`,
		"packages/web/tsconfig.json": `{"compilerOptions":{"baseUrl":".","paths":{"@web/*":["app/*"]}}}`,
		"src/lib/llm.ts": "import OpenAI from \"openai\";\nexport const c = new OpenAI();\n" +
			"export async function ask(p: string) {\n" +
			"  return c.chat.completions.create({ model: \"gpt-5.1\", messages: p });\n}\n",
	}
	// Enough importers that a per lookup parse would be obvious.
	for i := 0; i < 12; i++ {
		files[fmt.Sprintf("src/app/caller%d.ts", i)] =
			fmt.Sprintf("import { ask } from \"@lib/llm\";\nexport const r%d = ask(\"hi\");\n", i)
	}
	var fs []file
	for _, name := range sortedFileNames(files) {
		fs = append(fs, file{path: name, data: files[name]})
	}
	a := newAnalyzer(fs)
	for i := 0; i < 12; i++ {
		a.tsconfigResolve("@lib/llm")
		a.tsconfigResolve("@web/thing")
	}
	if got := a.tsParses.Load(); got != 2 {
		t.Errorf("parsed %d tsconfigs across 24 lookups, want the 2 in the tree", got)
	}
}

// sortedFileNames keeps the analyzer's input in the walk order it would
// see on disk, since alias resolution is order sensitive by design.
func sortedFileNames(files map[string]string) []string {
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
