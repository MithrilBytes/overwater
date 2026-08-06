package scan

import (
	"bytes"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// notebookToPython flattens a Jupyter notebook's code cells into one
// Python source, so notebooks scan like any other file. Line numbers
// refer to this flattened view; each cell is marked.
func notebookToPython(data []byte) []byte {
	var nb struct {
		Cells []struct {
			CellType string   `json:"cell_type"`
			Source   []string `json:"source"`
		} `json:"cells"`
	}
	if err := json.Unmarshal(data, &nb); err != nil {
		return data
	}
	var b strings.Builder
	for i, cell := range nb.Cells {
		if cell.CellType != "code" {
			continue
		}
		b.WriteString("# cell ")
		b.WriteString(strings.TrimSpace(string(rune('0' + i%10))))
		b.WriteString("\n")
		for _, line := range cell.Source {
			b.WriteString(line)
			if !strings.HasSuffix(line, "\n") {
				b.WriteString("\n")
			}
		}
	}
	return []byte(b.String())
}

type file struct {
	path string // slash separated, relative to root
	data []byte
}

// Directories that never hold first party call sites.
var skipDirs = map[string]bool{
	".git":           true,
	"node_modules":   true,
	"vendor":         true,
	"dist":           true,
	"build":          true,
	".next":          true,
	"__pycache__":    true,
	".venv":          true,
	"venv":           true,
	"site-packages":  true,
	"__pypackages__": true,
	".tox":           true,
	".gradle":        true,
	".dart_tool":     true,
	"target":         true,
	".idea":          true,
	".vscode":        true,
}

// Lock files repeat dependency names at enormous length; layer 1 reads
// the manifests instead.
var skipFiles = map[string]bool{
	"package-lock.json": true,
	"yarn.lock":         true,
	"pnpm-lock.yaml":    true,
	"poetry.lock":       true,
	"go.sum":            true,
	"Cargo.lock":        true,
	// Our own artifacts name model ids; scanning them would make the
	// tool flag its own output.
	".overwater.json": true,
	"MODELS.md":       true,
}

const maxFileSize = 512 * 1024

// walk lists the scannable files under root. A non nil only set
// restricts the result to the named root relative paths, which is how
// incremental scans skip unchanged files without reading them.
func walk(root string, only map[string]bool) ([]file, error) {
	var files []file
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path != root && skipDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if skipFiles[d.Name()] {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if only != nil && !only[rel] {
			return nil
		}
		info, err := d.Info()
		if err != nil || info.Size() > maxFileSize {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		if bytes.IndexByte(data, 0) >= 0 {
			return nil // binary
		}
		if strings.HasSuffix(d.Name(), ".ipynb") {
			data = notebookToPython(data)
		}
		files = append(files, file{path: rel, data: data})
		return nil
	})
	return files, err
}
