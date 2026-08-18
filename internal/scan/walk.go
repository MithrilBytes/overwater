package scan

import (
	"bytes"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// notebookToPython flattens a notebook's code cells into one Python
// source, so notebooks scan like any other file. Line numbers refer to
// the flattened view.
func notebookToPython(data []byte) []byte {
	var nb struct {
		Cells []struct {
			CellType string          `json:"cell_type"`
			Source   json.RawMessage `json:"source"`
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
		for _, line := range cellSource(cell.Source) {
			b.WriteString(line)
			if !strings.HasSuffix(line, "\n") {
				b.WriteString("\n")
			}
		}
	}
	return []byte(b.String())
}

// cellSource reads nbformat's multiline_string: the list of lines, or
// the single string they join to. Both spellings are legal and both are
// written, and typing the field as the list alone failed the unmarshal
// for the whole document, so one markdown cell written the other way
// discarded every code cell with it.
func cellSource(raw json.RawMessage) []string {
	var lines []string
	if json.Unmarshal(raw, &lines) == nil {
		return lines
	}
	var one string
	if json.Unmarshal(raw, &one) == nil {
		return []string{one}
	}
	return nil
}

// file is one walked source. data is a string, not the []byte it was
// read as, because the analyzer keeps it for the whole pass and a
// []byte here would mean the repository is resident twice.
type file struct {
	path string // slash separated, relative to root
	data string
}

// normalizeNewlines folds CRLF to LF at the one point where content
// enters the scanner, so a Windows checkout and a Unix one produce the
// same verdict for the same commit. Lone carriage returns stay: they
// are not line breaks in any language here, and rewriting them would
// shift line numbers.
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
	"third_party":    true,
	"Pods":           true,
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

// What the scanner keeps of a notebook is the flattened code, and the
// code is the small part of the file: one saved plot output is a base64
// image past maxFileSize on its own, so holding the raw .ipynb to that
// cap hid every notebook that had ever been run. The flattened source
// is still capped at maxFileSize below; this bounds only the read.
const maxNotebookSize = 8 * 1024 * 1024

// walk lists every scannable file under root. Incremental scans load
// the full set too; the analyzer, not the walker, decides which files
// produce sites, so full and incremental runs resolve alike.
func walk(root string) ([]file, error) {
	// WalkDir lstats its root, so a symlinked root arrives at the
	// callback as a symlink, is dropped like any other, and the scan
	// reports a repository it never opened. The rule below is about
	// links inside the tree, which have real paths of their own to be
	// reached by; the root is the caller's argument and has none.
	rootInfo, err := os.Stat(root)
	if err != nil {
		return nil, err
	}
	walkRoot := root
	if rootInfo.IsDir() {
		if resolved, err := filepath.EvalSymlinks(root); err == nil {
			walkRoot = resolved
		}
	}
	var files []file
	err = filepath.WalkDir(walkRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if d == nil {
				return err // the root itself is missing or unreadable
			}
			// One unreadable dir must not abort the whole scan.
			return nil
		}
		if d.IsDir() {
			if path != walkRoot && skipDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if skipFiles[d.Name()] {
			return nil
		}
		// Regular files only. FIFOs, sockets and devices would block or
		// fail ReadFile. Symlinks go too: the lstat size below says
		// nothing about the target, so a link could smuggle a file past
		// the size cap or point outside the root. Targets inside the root
		// are reached by their real paths.
		if !d.Type().IsRegular() {
			return nil
		}
		rel, err := filepath.Rel(walkRoot, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		notebook := strings.HasSuffix(d.Name(), ".ipynb")
		limit := int64(maxFileSize)
		if notebook {
			limit = maxNotebookSize
		}
		info, err := d.Info()
		if err != nil || info.Size() > limit {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		if bytes.IndexByte(data, 0) >= 0 {
			return nil // binary
		}
		if notebook {
			data = notebookToPython(data)
			if len(data) > maxFileSize {
				return nil
			}
		}
		files = append(files, file{path: rel, data: string(normalizeNewlines(data))})
		return nil
	})
	if err != nil {
		return nil, err
	}
	// Reading nothing is not the same as finding nothing, but it is not
	// an error either: an incremental run whose candidates were all
	// deleted legitimately scans zero files. The caller says so out
	// loud rather than reporting a clean bill of health in silence.
	return files, nil
}
