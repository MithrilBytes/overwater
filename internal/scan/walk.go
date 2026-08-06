package scan

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
)

type file struct {
	path string // slash separated, relative to root
	data []byte
}

// Directories that never hold first party call sites.
var skipDirs = map[string]bool{
	".git":         true,
	"node_modules": true,
	"vendor":       true,
	"dist":         true,
	"build":        true,
	".next":        true,
	"__pycache__":  true,
	".venv":        true,
	"venv":         true,
	"target":       true,
	".idea":        true,
	".vscode":      true,
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

func walk(root string) ([]file, error) {
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
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		files = append(files, file{path: filepath.ToSlash(rel), data: data})
		return nil
	})
	return files, err
}
