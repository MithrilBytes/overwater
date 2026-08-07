package scan

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// crlfSource is one call site whose system prompt is long enough that
// its measured size decides a finding. Written with LF; the test scans
// it twice, once converted to CRLF.
func crlfSource() string {
	var b strings.Builder
	b.WriteString("import anthropic\n\nclient = anthropic.Anthropic()\n\nSYSTEM = \"\"\"\n")
	for i := 0; i < 120; i++ {
		fmt.Fprintf(&b, "Rule %d: keep the answer short and cite the source document.\n", i)
	}
	b.WriteString("\"\"\"\n\n\ndef answer(question):\n" +
		"    return client.messages.create(\n" +
		"        model=\"claude-opus-4-5\",\n" +
		"        system=SYSTEM,\n" +
		"        max_tokens=1024,\n" +
		"        messages=[{\"role\": \"user\", \"content\": question}],\n" +
		"    )\n")
	return b.String()
}

func writeRepo(t *testing.T, name, content string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// Line endings must not change the verdict. Carriage returns used to
// count as prompt characters, so identical source scanned clean with LF
// and reported a finding with CRLF, making the answer depend on which
// platform checked the repo out.
func TestCRLFMatchesLF(t *testing.T) {
	lf := crlfSource()
	crlf := strings.ReplaceAll(lf, "\n", "\r\n")
	if !bytes.Contains([]byte(crlf), []byte("\r\n")) || strings.Contains(lf, "\r") {
		t.Fatal("the two inputs are not an LF and CRLF pair")
	}

	cat := mustCatalog(t)
	lfReport, err := Analyze(writeRepo(t, "answer.py", lf), cat)
	if err != nil {
		t.Fatal(err)
	}
	crlfReport, err := Analyze(writeRepo(t, "answer.py", crlf), cat)
	if err != nil {
		t.Fatal(err)
	}

	if len(lfReport.Sites) != 1 {
		t.Fatalf("LF scan found %d sites, want 1", len(lfReport.Sites))
	}
	got, want := crlfReport.Sites, lfReport.Sites
	if len(got) != len(want) {
		t.Fatalf("CRLF scan found %d sites, LF scan found %d", len(got), len(want))
	}
	for i := range want {
		w, g := want[i], got[i]
		if g.Shape.SystemPromptChars != w.Shape.SystemPromptChars {
			t.Errorf("site %d: prompt measured %d chars with CRLF, %d with LF",
				i, g.Shape.SystemPromptChars, w.Shape.SystemPromptChars)
		}
		if g.Hash != w.Hash {
			t.Errorf("site %d: hash %s with CRLF, %s with LF", i, g.Hash, w.Hash)
		}
		if g.Line != w.Line || g.Col != w.Col {
			t.Errorf("site %d: at %d:%d with CRLF, %d:%d with LF", i, g.Line, g.Col, w.Line, w.Col)
		}
		if !reflect.DeepEqual(g, w) {
			t.Errorf("site %d differs between line endings:\nCRLF %+v\n  LF %+v", i, g, w)
		}
	}
	// The prompt is what the rules measure, so it has to be the real
	// text and not an empty read that happens to match.
	if n := lfReport.Sites[0].Shape.SystemPromptChars; n < 4400 {
		t.Fatalf("system prompt measured %d chars; the test needs a long one", n)
	}
}

// Line and column still address the same source position after the fold.
func TestCRLFKeepsLineAndColumn(t *testing.T) {
	lf := "# comment\nmodel = \"gpt-5-mini\"\n"
	crlf := strings.ReplaceAll(lf, "\n", "\r\n")
	cat := mustCatalog(t)
	for _, tc := range []struct{ name, content string }{{"lf", lf}, {"crlf", crlf}} {
		report, err := Analyze(writeRepo(t, "conf.py", tc.content), cat)
		if err != nil {
			t.Fatal(err)
		}
		if len(report.Sites) != 1 {
			t.Fatalf("%s: found %d sites, want 1", tc.name, len(report.Sites))
		}
		if s := report.Sites[0]; s.Line != 2 || s.Col != 9 {
			t.Errorf("%s: reference at %d:%d, want 2:9", tc.name, s.Line, s.Col)
		}
	}
}
