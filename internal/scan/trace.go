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

var envReaderPatterns = []string{
	`process\.env\.%s\b`,
	`process\.env\[["']%s["']\]`,
	`os\.environ(?:\.get)?\(\s*["']%s["']`,
	`os\.environ\[\s*["']%s["']`,
	`os\.Getenv\(\s*["']%s["']`,
	`ENV\[\s*["']%s["']`,
	`System\.getenv\(\s*["']%s["']`,
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
	var cfgPaths []string
	for p := range a.byPath {
		if isConfigFile(p) {
			cfgPaths = append(cfgPaths, p)
		}
	}
	sort.Strings(cfgPaths)
	for _, cfgPath := range cfgPaths {
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
				r := a.regionFor(reader.path, reader.line, reader.col)
				site.Shape = a.extractShape(reader.path, r)
				site.Hash = a.siteHash(reader.path, reader.line, r)
				tier := ""
				if model != nil {
					tier = model.Tier
				}
				site.Archetype, site.ArchetypeConfidence = a.classify(reader.path, site.Shape, r, tier)
				site.Ignored, site.VolumeOverride = a.pragmas(reader.path, r)
				site.NearbyStrings = a.nearbyStrings(reader.path, r)
				report.Sites = append(report.Sites, site)
			}
		}
	}
}

// findEnvReaders locates up to two places that read the given env key,
// in deterministic path order.
func (a *analyzer) findEnvReaders(key, excludePath string) []readerLoc {
	var paths []string
	for p := range a.byPath {
		if p != excludePath && !isConfigFile(p) {
			paths = append(paths, p)
		}
	}
	sort.Strings(paths)
	var out []readerLoc
	for _, p := range paths {
		content := a.byPath[p]
		for _, pattern := range envReaderPatterns {
			re := regexp.MustCompile(fmt.Sprintf(pattern, regexp.QuoteMeta(key)))
			loc := re.FindStringIndex(content)
			if loc == nil {
				continue
			}
			line, col := offsetToLineCol(content, loc[0])
			out = append(out, readerLoc{path: p, line: line, col: col})
			break
		}
		if len(out) == 2 {
			break
		}
	}
	return out
}

func offsetToLineCol(content string, off int) (int, int) {
	starts := lineStarts(content)
	line := sort.Search(len(starts), func(i int) bool { return starts[i] > off })
	return line, off - starts[line-1]
}
