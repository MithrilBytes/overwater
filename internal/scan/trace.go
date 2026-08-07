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

// nearbyStrings collects short string literals inside the call region,
// raw material for drafted eval prompts. Local only, like everything
// else here.
func (a *analyzer) nearbyStrings(p string, r region) []string {
	content := a.byPath[p]
	spans := a.spansFor(p)
	// Spans come out of the scanner in increasing, non overlapping start
	// order, so the region's first candidate is a binary search away.
	// Walking the whole file's spans per call site is quadratic in a file
	// that holds thousands of strings.
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
// back to the calls that read it. The reader site carries the shape and
// the findings; the config site stays as the record of where the value
// lives. This is also the only detection path for Azure OpenAI style
// deployments, whose names are user chosen and never in the dictionary.
// A non nil only set restricts which reader files may produce sites;
// config files are read regardless, so incremental scans keep tracing.
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
			for _, r := range a.findEnvReaders(key, cfgPath) {
				if only != nil && !only[r.path] {
					continue
				}
				loc := r.path + ":" + strconv.Itoa(r.line)
				if existing[loc] {
					continue
				}
				existing[loc] = true
				site := Site{
					File: r.path, Line: r.line, Col: r.col,
					Ref: value, ViaConfig: cfgPath + " " + key,
				}
				if model != nil {
					site.Known = true
					site.ModelID = model.ID
				}
				reg := a.regionFor(r.path, r.line, r.col)
				site.Shape = a.extractShape(r.path, reg)
				site.Hash = a.siteHash(r.path, r.line, reg)
				tier := ""
				if model != nil {
					tier = model.Tier
				}
				site.Archetype, site.ArchetypeConfidence = a.classify(r.path, site.Shape, reg, tier)
				site.Ignored, site.VolumeOverride = a.pragmas(r.path, reg)
				site.NearbyStrings = a.nearbyStrings(r.path, reg)
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
