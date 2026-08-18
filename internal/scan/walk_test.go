package scan

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// walkTemp writes a tree and walks it, returning the files by path.
func walkTemp(t *testing.T, files map[string]string) map[string]string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		full := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	walked, err := walk(dir)
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	out := map[string]string{}
	for _, f := range walked {
		out[f.path] = f.data
	}
	return out
}

const notebookCall = `    "client.messages.create(\n",
    "    model=\"claude-haiku-4-5\",\n",
    "    max_tokens=300,\n",
    ")\n"`

// A notebook is mostly its outputs: one saved plot is a base64 PNG
// larger than the whole file cap, so capping the raw file hid every
// notebook that had ever been run, and the scan still read as clean.
func TestNotebookWithSavedOutputStillScans(t *testing.T) {
	plot := strings.Repeat("iVBORw0KGgoAAAANSUhEUg", 40000) // ~880KB, past maxFileSize
	nb := `{"cells": [
  {"cell_type": "code", "source": [
` + notebookCall + `
   ],
   "outputs": [{"output_type": "display_data", "data": {"image/png": "` + plot + `"}}]}
]}`
	files := walkTemp(t, map[string]string{"analysis.ipynb": nb})
	got, ok := files["analysis.ipynb"]
	if !ok {
		t.Fatalf("walked %v, want the notebook", walkedPaths(files))
	}
	if !strings.Contains(got, `model="claude-haiku-4-5"`) {
		t.Errorf("flattened notebook = %q, want the call in it", got)
	}
	if strings.Contains(got, "iVBORw0KGgo") {
		t.Error("the saved output survived flattening")
	}
}

// nbformat spells a cell's source as a list of lines or as the one
// string they join to, and jupyter writes both. Reading only the list
// failed the unmarshal for the whole document, so a single markdown
// cell written the other way discarded every code cell with it.
func TestNotebookStringCellSource(t *testing.T) {
	nb := `{"cells": [
  {"cell_type": "markdown", "source": "# analysis\n"},
  {"cell_type": "code", "source": [
` + notebookCall + `
  ]}
]}`
	got := walkTemp(t, map[string]string{"analysis.ipynb": nb})["analysis.ipynb"]
	if strings.Contains(got, "cell_type") {
		t.Fatalf("notebook was left as raw json: %q", got)
	}
	if !strings.Contains(got, `model="claude-haiku-4-5"`) {
		t.Errorf("flattened notebook = %q, want the call in it", got)
	}
}

// The same, with the code cell itself written as one string.
func TestNotebookStringCodeCell(t *testing.T) {
	nb := `{"cells": [
  {"cell_type": "code", "source": "client.messages.create(\n    model=\"claude-haiku-4-5\",\n    max_tokens=300,\n)\n"}
]}`
	got := walkTemp(t, map[string]string{"analysis.ipynb": nb})["analysis.ipynb"]
	if !strings.Contains(got, `model="claude-haiku-4-5"`) {
		t.Errorf("flattened notebook = %q, want the call in it", got)
	}
	if !strings.Contains(got, "max_tokens=300") {
		t.Errorf("flattened notebook = %q, want the cell's later lines too", got)
	}
}

// third_party and Pods are dependency trees like vendor and
// node_modules: a vendored SDK carries its own model roster, and priced
// as first party it is spend the repository never makes.
func TestWalkSkipsVendoredTrees(t *testing.T) {
	files := walkTemp(t, map[string]string{
		"app.py":                         "model = \"gpt-4o\"\n",
		"third_party/openai/models.py":   "MODELS = [\"gpt-4o\", \"gpt-4o-mini\"]\n",
		"Pods/AnthropicSDK/Models.swift": "let models = [\"claude-sonnet-5\"]\n",
	})
	if len(files) != 1 || files["app.py"] == "" {
		t.Errorf("walked %v, want only app.py", walkedPaths(files))
	}
}

func walkedPaths(m map[string]string) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}
