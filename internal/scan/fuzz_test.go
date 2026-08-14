package scan

import (
	"strings"
	"testing"

	"github.com/MithrilBytes/overwater/catalog"
)

// Fuzz targets: no panics anywhere in the per file machinery, and the
// span invariant (start <= interiorStart <= interiorEnd <= end, all in
// bounds) holds for every input. Seeds live under testdata/fuzz and run
// in normal go test.

func checkSpans(t *testing.T, p, content string) {
	t.Helper()
	for _, s := range scanSpans(content, familyFor(p)) {
		if !(0 <= s.start && s.start <= s.end && s.end <= len(content)) {
			t.Fatalf("%s: span bounds broken: %+v (len %d)", p, s, len(content))
		}
		// Interiors belong to string spans; comments never carry them.
		if s.kind == spanString && !(s.start <= s.interiorStart &&
			s.interiorStart <= s.interiorEnd && s.interiorEnd <= s.end) {
			t.Fatalf("%s: span interior invariant broken: %+v (len %d)", p, s, len(content))
		}
	}
}

func FuzzMaskFile(f *testing.F) {
	f.Add("a.py", "m = \"gpt-4o\"\nx = \"\"\"a\n")
	f.Add("a.py", "'''ab")
	f.Add("a.ts", "`")
	f.Add("a.ts", "\"abc")
	f.Add("a.go", "var p = `C:\\dir\\`\nvar q = \"x\"\n")
	f.Add("a.js", "// comment\nconst s = 'it\\'s';\n/* block */\n")
	f.Fuzz(func(t *testing.T, p, content string) {
		m := maskFile(p, content)
		if len(m.all) != len(content) || len(m.prose) != len(content) {
			t.Fatal("masking changed the length")
		}
		if strings.Count(m.all, "\n") != strings.Count(content, "\n") ||
			strings.Count(m.prose, "\n") != strings.Count(content, "\n") {
			t.Fatal("masking changed line structure")
		}
		checkSpans(t, p, content)
	})
}

func FuzzParseProps(f *testing.F) {
	f.Add("{model: \"gpt-4o\", temperature: 0.2, nested: {a: 1}}", 1, 47)
	f.Add("(model=\"claude-sonnet-5\", max_tokens=32_768)", 1, 43)
	f.Add("{note: \"colon : and brace { inside\", after: 1}", 1, 45)
	f.Add("{}", -5, 99)
	f.Fuzz(func(t *testing.T, content string, start, end int) {
		m := maskFile("f.ts", content)
		props := parseProps(content, m.all, m.prose, start, end)
		for k, v := range props {
			_ = k
			_ = v
		}
	})
}

func FuzzCallExtent(f *testing.F) {
	f.Add("await create({\n  model: \"gpt-5.1\",\n  max_tokens: 32,\n});\n", 25)
	f.Add("streamText({ model: anthropic(\"claude-opus-5\"), system: \"s\" })", 35)
	f.Add("((((((((", 4)
	f.Add(")}]", 1)
	f.Add("x = \"\"\"a", 3)
	f.Fuzz(func(t *testing.T, content string, hit int) {
		// Contract: hit is a byte offset into the file, [0, len].
		if hit < 0 {
			hit = -hit
		}
		if len(content) == 0 {
			hit = 0
		} else {
			hit %= len(content) + 1
		}
		m := maskFile("f.ts", content)
		if s, e, ok := callExtent(m.all, m.prose, hit); ok {
			if !(0 <= s && s <= e && e <= len(content)) {
				t.Fatalf("callExtent bounds broken: [%d, %d) len %d", s, e, len(content))
			}
		}
		if s, e, ok := innermostExtent(m.all, hit); ok {
			if !(0 <= s && s <= e && e <= len(content)) {
				t.Fatalf("innermostExtent bounds broken: [%d, %d) len %d", s, e, len(content))
			}
		}
		if s, e, ok := builderExtent(m.all, hit); ok {
			if !(0 <= s && s <= e && e <= len(content)) {
				t.Fatalf("builderExtent bounds broken: [%d, %d) len %d", s, e, len(content))
			}
		}
	})
}

func FuzzAnalyzeFile(f *testing.F) {
	cat, err := catalog.Embedded()
	if err != nil {
		f.Fatal(err)
	}
	names := cat.Names()
	f.Add("panic.py", "m = \"gpt-4o\"\nx = \"\"\"a\n")
	f.Add("call.py", "def summarize(t):\n    return client.messages.create(\n        model=\"claude-sonnet-5\",\n        max_tokens=300,\n        system=\"Condense the report.\",\n    )\n")
	f.Add("raw.go", "package x\n\nvar d = `a\\`\nvar m = \"claude-haiku-4-5\"\n")
	f.Add("cfg.yaml", "llm:\n  model: gpt-5-mini\n  max_completion_tokens: 700\n")
	f.Add("tick.ts", "const m = \"gpt-5.1\";\nconst t = `")
	f.Add("env.env", "MODEL=gpt-4o\n")
	f.Fuzz(func(t *testing.T, p, content string) {
		if len(content) > 1<<16 {
			t.Skip("oversized input")
		}
		fl := file{path: p, data: content}
		a := newAnalyzer([]file{fl})
		sites, _ := a.analyzeFile(fl, names)
		for _, s := range sites {
			if s.Line < 1 || s.Col < 0 {
				t.Fatalf("site out of bounds: %+v", s)
			}
		}
		checkSpans(t, p, content)
	})
}
