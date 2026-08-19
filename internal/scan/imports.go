package scan

import (
	"encoding/json"
	"path"
	"regexp"
	"sort"
	"strings"
)

// The import forms that can bring a named constant into a file.
var (
	reImportJS   = regexp.MustCompile(`import\s*\{([^}]+)\}\s*from\s*["'](\.{1,2}/[\w./-]+)["']`)
	reExportFrom = regexp.MustCompile(`export\s*\{([^}]+)\}\s*from\s*["'](\.{1,2}/[\w./-]+)["']`)
	reImportBare = regexp.MustCompile(`import\s*\{([^}]+)\}\s*from\s*["']([@\w][\w./-]*)["']`)
	reRequireJS  = regexp.MustCompile(`(?:const|let|var)\s*\{([^}]+)\}\s*=\s*require\(\s*["'](\.{1,2}/[\w./-]+)["']\s*\)`)
	rePyImport   = regexp.MustCompile(`from\s+([\w.]+)\s+import\s+([\w, ]+)`)
)

// importTargets lists candidate repo paths that might define name,
// from the file's own import statements, tsconfig path aliases, and a
// suffix search as the last resort for workspace layouts.
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

// tsAliases is one tsconfig's path map, flattened and ordered so a
// lookup is a walk rather than a parse.
type tsAliases struct {
	base     string
	patterns []string
	targets  map[string][]string
}

// tsconfigs parses every tsconfig in the repository once per pass. It
// used to be parsed once per lookup, which on a monorepo is every config
// re-read for every non relative import in every file. Configs and
// patterns are ordered here, so ambiguous aliases resolve the same way
// on every run.
func (a *analyzer) tsconfigs() []tsAliases {
	a.tsOnce.Do(func() {
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
			a.tsParses.Add(1)
			if err := json.Unmarshal([]byte(jsonStripComments(a.byPath[known])), &cfg); err != nil {
				continue
			}
			patterns := make([]string, 0, len(cfg.CompilerOptions.Paths))
			for pattern := range cfg.CompilerOptions.Paths {
				patterns = append(patterns, pattern)
			}
			sort.Strings(patterns)
			a.tsCfgs = append(a.tsCfgs, tsAliases{
				base:     path.Join(path.Dir(known), cfg.CompilerOptions.BaseURL),
				patterns: patterns,
				targets:  cfg.CompilerOptions.Paths,
			})
		}
	})
	return a.tsCfgs
}

// tsconfigResolve expands a non relative import spec through the
// compilerOptions paths of every tsconfig.json in the repo.
func (a *analyzer) tsconfigResolve(spec string) []string {
	var out []string
	for _, cfg := range a.tsconfigs() {
		for _, pattern := range cfg.patterns {
			prefix := strings.TrimSuffix(pattern, "*")
			if !strings.HasPrefix(spec, prefix) {
				continue
			}
			rest := strings.TrimPrefix(spec, prefix)
			for _, sub := range cfg.targets[pattern] {
				out = append(out, path.Join(cfg.base, strings.TrimSuffix(sub, "*")+rest))
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
