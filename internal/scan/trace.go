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
	reConfigKV     = regexp.MustCompile(`^\s*"?([A-Z][A-Z0-9_]*)"?\s*[:=]\s*["']?([A-Za-z0-9][\w./:-]{2,60})`)
)

func (a *analyzer) pragmas(p string, regionStart, regionEnd int) (bool, int) {
	content := a.byPath[p]
	from := linesAbove(content, regionStart, 3)
	region := content[from:min(regionEnd, len(content))]
	ignored := rePragmaIgnore.MatchString(region)
	volume := 0
	if m := rePragmaVolume.FindStringSubmatch(region); m != nil {
		if v, err := strconv.Atoi(m[1]); err == nil {
			volume = v
		}
	}
	return ignored, volume
}

func (a *analyzer) spansFor(p string) []span {
	a.mu.Lock()
	s, ok := a.spans[p]
	a.mu.Unlock()
	if ok {
		return s
	}
	s = scanSpans(a.byPath[p], familyFor(p))
	a.mu.Lock()
	a.spans[p] = s
	a.mu.Unlock()
	return s
}

// nearbyStrings collects short string literals inside the call region,
// raw material for drafted eval prompts. Local only, like everything
// else here.
func (a *analyzer) nearbyStrings(p string, regionStart, regionEnd int) []string {
	content := a.byPath[p]
	var out []string
	for _, s := range a.spansFor(p) {
		if s.kind != spanString || s.interiorEnd <= regionStart || s.interiorStart >= regionEnd {
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
func (a *analyzer) traceConfigModels(report *Report, names map[string]*catalog.Model) {
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
			if !strings.Contains(key, "MODEL") && !strings.Contains(key, "DEPLOYMENT") {
				continue
			}
			model := names[value]
			// Unknown values must at least look like a model or
			// deployment name, not a bare word.
			if model == nil && !strings.ContainsAny(value, "-0123456789") {
				continue
			}
			for _, r := range a.findEnvReaders(key, cfgPath) {
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
				rs, re, es, ok := a.regionFor(r.path, r.line, r.col)
				site.Shape = a.extractShape(r.path, rs, re, es, ok)
				site.Hash = a.siteHash(r.path, r.line, rs, re, ok)
				tier := ""
				if model != nil {
					tier = model.Tier
				}
				hit := hitOffset(a.byPath[r.path], r.line, r.col)
				site.Archetype, site.ArchetypeConfidence = a.classify(r.path, site.Shape, rs, re, hit, tier)
				site.Ignored, site.VolumeOverride = a.pragmas(r.path, rs, re)
				site.NearbyStrings = a.nearbyStrings(r.path, rs, re)
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
