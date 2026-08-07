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

// normalizeNewlines folds CRLF to LF at the one point where file content
// enters the scanner, so every offset, line number, span, hash, and
// measurement downstream sees one canonical form. Left in, the carriage
// returns count as prompt characters, and the same commit gets a
// different verdict on a Windows checkout than on a Unix one. Lone
// carriage returns are left alone: they are not line breaks to any of
// the languages here, and rewriting them would change line numbering.
func normalizeNewlines(data []byte) []byte {
	if !bytes.Contains(data, []byte("\r\n")) {
		return data
	}
	return bytes.ReplaceAll(data, []byte("\r\n"), []byte("\n"))
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
	".overwater.yaml": true,
	"MODELS.md":       true,
}

const maxFileSize = 512 * 1024

// walk lists every scannable file under root. Incremental scans load
// the full set too: which files produce sites is decided by the
// analyzer, never by skipping context files here, so resolution power
// stays identical between full and incremental runs.
func walk(root string) ([]file, error) {
	var files []file
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if d == nil {
				return err // the root itself is missing or unreadable
			}
			// An unreadable subdirectory or entry is skipped, never
			// fatal: one chmod 000 dir must not abort the whole scan.
			return nil
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
		// Only regular files scan. Irregular files (FIFOs, sockets,
		// devices) would block or fail ReadFile; symlinks are skipped
		// outright because the lstat size below says nothing about the
		// target, so a link could smuggle a file past the size cap or
		// point outside the root. Targets inside the root are reached
		// by their real paths anyway.
		if !d.Type().IsRegular() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
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
		files = append(files, file{path: rel, data: normalizeNewlines(data)})
		return nil
	})
	return files, err
}
