package scan

import (
	"strings"
	"testing"
)

func TestMaskFilePreservesOffsets(t *testing.T) {
	content := "const A = `line one\nline two`;\n// trailing comment\n"
	m := maskFile("a.ts", content)
	if len(m.all) != len(content) || len(m.prose) != len(content) {
		t.Fatal("masking changed the length")
	}
	if strings.Count(m.all, "\n") != strings.Count(content, "\n") {
		t.Fatal("masking changed line structure")
	}
}

func TestMaskFileBlanksCommentsAndLongStrings(t *testing.T) {
	long := strings.Repeat("prose about temperature settings ", 4)
	content := "// comment with max_tokens: 99\nconst SHORT = \"input_schema\";\nconst LONG = \"" + long + "\";\n"
	m := maskFile("a.ts", content)
	if strings.Contains(m.prose, "max_tokens") {
		t.Error("comment text survived prose masking")
	}
	if !strings.Contains(m.prose, "input_schema") {
		t.Error("short syntax string should survive prose masking")
	}
	if strings.Contains(m.prose, "temperature") {
		t.Error("long prose string survived prose masking")
	}
	if strings.Contains(m.all, "input_schema") {
		t.Error("string interior survived full masking")
	}
}

func TestMaskFilePythonTriples(t *testing.T) {
	content := "PROMPT = \"\"\"docstring with temperature: 0.7 inside and quite a lot of extra words\"\"\"\nx = 1  # temperature: 0.2\n"
	m := maskFile("a.py", content)
	if strings.Contains(m.prose, "temperature") {
		t.Errorf("prose = %q, want prompt and comment text blanked", m.prose)
	}
}
