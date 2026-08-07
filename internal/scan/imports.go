package scan

import (
	"encoding/json"
	"path"
	"regexp"
	"sort"
	"strings"
)

// and a suffix search as the last resort for workspace layouts.
func (a *analyzer) importTargets(p, name string) []string {
	content := a.byPath[p]
	dir := path.Dir(p)
	var targets []string
	jsExts := []string{".ts", ".tsx", ".js", ".jsx", ".mjs", ".cjs"}
	add := func(spec string, exts []string) {
		base := path.Clean(path.Join(dir, spec))
		for _, ext := range exts {
			targets = append(targets, base+ext)
		}
	}
	addAliased := func(spec string) {
		for _, resolved := range a.tsconfigResolve(spec) {
			for _, ext := range jsExts {
				targets = append(targets, path.Clean(resolved)+ext)
			}
		}
		// Workspace fallback: any repo file whose path ends with the
		// spec, tried with each extension, in sorted path order so
		// ambiguous candidates resolve the same way on every run.
		for _, ext := range jsExts {
			suffix := spec + ext
			for _, known := range a.paths {
				if strings.HasSuffix(known, suffix) {
					targets = append(targets, known)
				}
			}
		}
	}
	for _, re := range []*regexp.Regexp{reImportJS, reExportFrom, reRequireJS, reImportBare} {
		for _, m := range re.FindAllStringSubmatch(content, -1) {
			if !importsName(m[1], name) {
				continue
			}
			if strings.HasPrefix(m[2], ".") {
				add(m[2], jsExts)
			} else {
				addAliased(m[2])
			}
		}
	}
	for _, m := range rePyImport.FindAllStringSubmatch(content, -1) {
		if importsName(m[2], name) {
			module := strings.TrimLeft(m[1], ".")
			spec := strings.ReplaceAll(module, ".", "/")
			add(spec, []string{".py"})
			targets = append(targets, path.Clean(spec)+".py")
		}
	}
	return targets
}

// tsconfigResolve expands a non relative import spec through the
// compilerOptions paths of any tsconfig.json in the repo. Configs and
// their path patterns are visited in sorted order, keeping ambiguous
// alias resolution stable across runs.
func (a *analyzer) tsconfigResolve(spec string) []string {
	var out []string
	for _, known := range a.paths {
		if path.Base(known) != "tsconfig.json" && path.Base(known) != "jsconfig.json" {
			continue
		}
		var cfg struct {
			CompilerOptions struct {
				BaseURL string              `json:"baseUrl"`
				Paths   map[string][]string `json:"paths"`
			} `json:"compilerOptions"`
		}
		if err := json.Unmarshal([]byte(jsonStripComments(a.byPath[known])), &cfg); err != nil {
			continue
		}
		root := path.Dir(known)
		base := path.Join(root, cfg.CompilerOptions.BaseURL)
		patterns := make([]string, 0, len(cfg.CompilerOptions.Paths))
		for pattern := range cfg.CompilerOptions.Paths {
			patterns = append(patterns, pattern)
		}
		sort.Strings(patterns)
		for _, pattern := range patterns {
			prefix := strings.TrimSuffix(pattern, "*")
			if !strings.HasPrefix(spec, prefix) {
				continue
			}
			rest := strings.TrimPrefix(spec, prefix)
			for _, sub := range cfg.CompilerOptions.Paths[pattern] {
				out = append(out, path.Join(base, strings.TrimSuffix(sub, "*")+rest))
			}
		}
	}
	return out
}

func importsName(list, name string) bool {
	for _, part := range strings.Split(list, ",") {
		if strings.TrimSpace(part) == name {
			return true
		}
	}
	return false
}
