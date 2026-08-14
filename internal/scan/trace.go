package scan

import (
	"fmt"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/MithrilBytes/overwater/catalog"
)

// Pragmas, nearby string capture, and config to reader tracing.

var (
	rePragmaIgnore = regexp.MustCompile(`overwater:ignore\b`)
	rePragmaVolume = regexp.MustCompile(`overwater:volume=([0-9]+)`)
	reConfigKV     = regexp.MustCompile(`^\s*"?([A-Za-z][A-Za-z0-9_]*)"?\s*[:=]\s*["']?([A-Za-z0-9][\w./:-]{2,60})`)
)

func (a *analyzer) pragmas(p string, r region) (bool, int) {
	content := a.byPath[p]
	from := linesAbove(content, r.start, 3)
	text := content[from:min(r.end, len(content))]
	ignored := rePragmaIgnore.MatchString(text)
	volume := 0
	if m := rePragmaVolume.FindStringSubmatch(text); m != nil {
		if v, err := strconv.Atoi(m[1]); err == nil {
			volume = v
		}
	}
	return ignored, volume
}

// nearbyStrings collects short string literals inside the region, raw
// material for drafted eval prompts.
func (a *analyzer) nearbyStrings(p string, r region) []string {
	content := a.byPath[p]
	spans := a.spans(p)
	// Spans come out sorted and non overlapping, so the first candidate
	// is a binary search away. Walking every span per call site is
	// quadratic in a file that holds thousands of strings.
	first := sort.Search(len(spans), func(i int) bool { return spans[i].end > r.start })
	var out []string
	for _, s := range spans[first:] {
		if s.start >= r.end {
			break
		}
		if s.kind != spanString || s.interiorEnd <= r.start || s.interiorStart >= r.end {
			continue
		}
		text := strings.TrimSpace(content[s.interiorStart:s.interiorEnd])
		if n := len(text); n < 20 || n > 300 {
			continue
		}
		out = append(out, text)
		if len(out) == 5 {
			break
		}
	}
	return out
}

func isConfigFile(p string) bool {
	if strings.HasPrefix(path.Base(p), ".env") {
		return true
	}
	switch path.Ext(p) {
	case ".env", ".yaml", ".yml", ".toml", ".ini", ".properties":
		return true
	}
	return false
}

// envReaderPattern is one way source code reads an environment variable.
// prefix is the literal every match of template must begin with, so a
// file that does not contain it cannot match and never needs the regex.
type envReaderPattern struct {
	prefix   string
	template string
}

var envReaderPatterns = []envReaderPattern{
	{"process.env.", `process\.env\.%s\b`},
	{"process.env[", `process\.env\[["']%s["']\]`},
	{"os.environ", `os\.environ(?:\.get)?\(\s*["']%s["']`},
	{"os.environ[", `os\.environ\[\s*["']%s["']`},
	{"os.Getenv(", `os\.Getenv\(\s*["']%s["']`},
	{"ENV[", `ENV\[\s*["']%s["']`},
	{"System.getenv(", `System\.getenv\(\s*["']%s["']`},
}

// envCandidate is a non config file that mentions at least one reader
// syntax, in sorted path order. Every other file in the repo is dropped
// once here instead of being re-read for every config key, which on a
// repo the size of vscode is thousands of full corpus walks.
type envCandidate struct {
	path    string
	content string
	// prefixes has bit i set when envReaderPatterns[i].prefix appears in
	// content. The other patterns cannot match, whatever the key.
	prefixes uint
}

// envReaders builds the candidate list once per pass.
func (a *analyzer) envReaders() []envCandidate {
	a.envOnce.Do(func() {
		for _, p := range a.paths { // already sorted
			if isConfigFile(p) {
				continue
			}
			content := a.byPath[p]
			var prefixes uint
			for i, pat := range envReaderPatterns {
				if strings.Contains(content, pat.prefix) {
					prefixes |= 1 << i
				}
			}
			if prefixes == 0 {
				continue
			}
			a.envCands = append(a.envCands, envCandidate{path: p, content: content, prefixes: prefixes})
		}
	})
	return a.envCands
}

type readerLoc struct {
	path      string
	line, col int
}

// traceConfigModels ties a MODEL or DEPLOYMENT value in a config file
// back to the calls that read it; the reader site carries the shape and
// the findings. This is the only detection path for Azure OpenAI style
// deployments, whose names are user chosen and never in the catalog. A
// non nil only set restricts which readers may produce sites, but
// config files are read regardless so incremental scans keep tracing.
func (a *analyzer) traceConfigModels(report *Report, names map[string]*catalog.Model, only map[string]bool) {
	existing := map[string]bool{}
	for _, s := range report.Sites {
		existing[s.File+":"+strconv.Itoa(s.Line)] = true
	}
	for _, cfgPath := range a.paths { // already sorted
		if !isConfigFile(cfgPath) {
			continue
		}
		for _, line := range strings.Split(a.byPath[cfgPath], "\n") {
			m := reConfigKV.FindStringSubmatch(line)
			if m == nil {
				continue
			}
			key, value := m[1], m[2]
			upper := strings.ToUpper(key)
			if !strings.Contains(upper, "MODEL") && !strings.Contains(upper, "DEPLOYMENT") {
				continue
			}
			model := names[value]
			if model == nil {
				// Uppercase env style keys may name user chosen
				// deployments, which at least must look like one, not
				// a bare word. Lowercase yaml style keys must resolve
				// to a known model, which keeps prose out.
				if key != upper || !strings.ContainsAny(value, "-0123456789") {
					continue
				}
			}
			for _, reader := range a.findEnvReaders(key, cfgPath) {
				if only != nil && !only[reader.path] {
					continue
				}
				loc := reader.path + ":" + strconv.Itoa(reader.line)
				if existing[loc] {
					continue
				}
				existing[loc] = true
				site := Site{
					File: reader.path, Line: reader.line, Col: reader.col,
					Ref: value, ViaConfig: cfgPath + " " + key,
				}
				if model != nil {
					site.Known = true
					site.ModelID = model.ID
				}
				tier := ""
				if model != nil {
					tier = model.Tier
				}
				a.describe(&site, tier)
				report.Sites = append(report.Sites, site)
			}
		}
	}
}

// findEnvReaders locates up to two places that read the given env key,
// in deterministic path order.
func (a *analyzer) findEnvReaders(key, excludePath string) []readerLoc {
	// Compiled per key, not per file: every pattern embeds the quoted key
	// literally, so the same seven regexes serve the whole corpus. Lazily,
	// because most keys are read by nobody and compile nothing at all.
	res := make([]*regexp.Regexp, len(envReaderPatterns))
	var out []readerLoc
	for _, c := range a.envReaders() {
		// A match spells the key out, quoting or not, so a file that never
		// mentions it cannot match any pattern. Substring search costs a
		// fraction of the regex it replaces.
		if c.path == excludePath || !strings.Contains(c.content, key) {
			continue
		}
		for i, pattern := range envReaderPatterns {
			if c.prefixes&(1<<i) == 0 {
				continue
			}
			if res[i] == nil {
				res[i] = regexp.MustCompile(fmt.Sprintf(pattern.template, regexp.QuoteMeta(key)))
				a.envCompiles.Add(1)
			}
			loc := res[i].FindStringIndex(c.content)
			if loc == nil {
				continue
			}
			line, col := a.offsetToLineCol(c.path, loc[0])
			out = append(out, readerLoc{path: c.path, line: line, col: col})
			break
		}
		if len(out) == 2 {
			break
		}
	}
	return out
}

func (a *analyzer) offsetToLineCol(p string, off int) (int, int) {
	starts := a.lineStarts(p)
	line := sort.Search(len(starts), func(i int) bool { return int(starts[i]) > off })
	return line, off - int(starts[line-1])
}
