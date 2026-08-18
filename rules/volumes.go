package rules

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/MithrilBytes/overwater/catalog"
	"github.com/MithrilBytes/overwater/internal/scan"
)

// Where a call site's monthly call count came from. Only measured is
// real traffic; the rest are assumptions of decreasing specificity.
const (
	VolumeMeasured = "measured"
	VolumePragma   = "pragma"
	VolumeConfig   = "config"
	VolumeFlag     = "flag"
	VolumeFanIn    = "fan-in"
	VolumeEstimate = "estimate"
)

// Volumes is a measured traffic file. Site keys are "file:line" with
// the file relative to the scan root; model keys are catalog ids or the
// model string as the source writes it.
type Volumes struct {
	Sites  map[string]int `json:"sites,omitempty"`
	Models map[string]int `json:"models,omitempty"`
}

// ParseVolumes decodes a volumes file. An unknown field, an unparsable
// site key, or a negative count is an error: a file that half applies
// prices the report wrong and says nothing.
func ParseVolumes(raw []byte) (*Volumes, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var v Volumes
	if err := dec.Decode(&v); err != nil {
		return nil, err
	}
	if dec.More() {
		return nil, fmt.Errorf("trailing data after the volumes object")
	}
	keys := make([]string, 0, len(v.Sites))
	for key := range v.Sites {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if _, _, ok := splitSiteKey(key); !ok {
			return nil, fmt.Errorf("sites key %q is not file:line", key)
		}
		if v.Sites[key] < 0 {
			return nil, fmt.Errorf("sites key %q has a negative count", key)
		}
	}
	models := make([]string, 0, len(v.Models))
	for key := range v.Models {
		models = append(models, key)
	}
	sort.Strings(models)
	for _, key := range models {
		if key == "" {
			return nil, fmt.Errorf("models has an empty key")
		}
		if v.Models[key] < 0 {
			return nil, fmt.Errorf("models key %q has a negative count", key)
		}
	}
	if len(v.Sites)+len(v.Models) == 0 {
		return nil, fmt.Errorf("volumes file names no sites and no models")
	}
	return &v, nil
}

// JSON renders a volumes file in the shape ParseVolumes reads.
func (v *Volumes) JSON() ([]byte, error) {
	out, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(out, '\n'), nil
}

func splitSiteKey(key string) (string, int, bool) {
	i := strings.LastIndex(key, ":")
	if i <= 0 || i == len(key)-1 {
		return "", 0, false
	}
	line, err := strconv.Atoi(key[i+1:])
	if err != nil || line <= 0 {
		return "", 0, false
	}
	return key[:i], line, true
}

func siteKey(site scan.Site) string {
	return site.File + ":" + strconv.Itoa(site.Line)
}

// volume is one call site's monthly call count and where it came from.
// callers is how many callers fan in counted into calls, 0 when the
// count is one call site's own.
type volume struct {
	calls   int
	source  string
	callers int
}

// siteVolumes binds a volumes file to one report. A model key spreads
// its count evenly over the priced sites on that model that no site
// key already answers for; the denominator is settled before any site
// is priced, so visit order does not change it.
type siteVolumes struct {
	e *Engine
	v *Volumes
	// shares is model key, then site key, to that site's slice of the
	// count. Keyed by both because two models can sit on one line.
	shares    map[string]map[string]int
	unmatched []string
}

// bindVolumes prepares the volume lookup for one report. Ignored sites
// count toward a model key's split: an ignore pragma silences findings,
// not traffic.
func (e *Engine) bindVolumes(report *scan.Report, cat *catalog.Catalog) *siteVolumes {
	sv := &siteVolumes{e: e, v: e.Volumes}
	if sv.v == nil {
		return sv
	}
	names := cat.Names()
	drawn := map[string][]string{}
	used := map[string]bool{}
	hitSites := map[string]bool{}
	for _, site := range report.Sites {
		m := names[site.Ref]
		if !site.Known || m == nil {
			continue
		}
		key := sv.modelKey(site, m)
		if key != "" {
			used[key] = true
		}
		// A site named in the file is priced from its own count and
		// never reads a share, so leaving it in the denominator would
		// shrink everyone else's for nothing.
		if _, ok := sv.v.Sites[siteKey(site)]; ok {
			hitSites[siteKey(site)] = true
			continue
		}
		if key != "" {
			drawn[key] = append(drawn[key], siteKey(site))
		}
	}
	sv.shares = make(map[string]map[string]int, len(drawn))
	for key, sites := range drawn {
		// The odd calls an even split cannot place go to the first
		// sites in report order rather than off the report entirely.
		share, odd := sv.v.Models[key]/len(sites), sv.v.Models[key]%len(sites)
		sv.shares[key] = make(map[string]int, len(sites))
		for i, s := range sites {
			sv.shares[key][s] = share
			if i < odd {
				sv.shares[key][s]++
			}
		}
	}
	for key := range sv.v.Sites {
		if !hitSites[key] {
			sv.unmatched = append(sv.unmatched, fmt.Sprintf("no call site at %s", key))
		}
	}
	for key := range sv.v.Models {
		if !used[key] {
			sv.unmatched = append(sv.unmatched, fmt.Sprintf("no call site uses model %s", key))
		}
	}
	sort.Strings(sv.unmatched)
	return sv
}

// modelKey is the models key this site answers to: its catalog id
// first, then the string as written, so an alias in the file still
// lands on the site that spells it that way.
func (sv *siteVolumes) modelKey(site scan.Site, m *catalog.Model) string {
	if sv.v == nil {
		return ""
	}
	for _, key := range []string{m.ID, site.Ref} {
		if _, ok := sv.v.Models[key]; ok {
			return key
		}
	}
	return ""
}

// forSite resolves one call site's volume. Measured traffic beats a
// hand written pragma, and a site key beats a model key. Both are
// counts of this call site's own traffic, so fan in leaves them alone;
// it multiplies only the per site assumption the estimate, the config,
// and --volume supply.
func (sv *siteVolumes) forSite(site scan.Site, m *catalog.Model) volume {
	if sv.v != nil {
		if n, ok := sv.v.Sites[siteKey(site)]; ok {
			return volume{calls: n, source: VolumeMeasured}
		}
		if key := sv.modelKey(site, m); key != "" {
			if n, ok := sv.shares[key][siteKey(site)]; ok {
				return volume{calls: n, source: VolumeMeasured}
			}
		}
	}
	if site.VolumeOverride > 0 {
		return volume{calls: site.VolumeOverride, source: VolumePragma}
	}
	per := sv.e.Est.Volume.CallsPerMonth
	if n := sv.e.callers(site); n > 1 {
		return volume{calls: per * n, source: VolumeFanIn, callers: n}
	}
	return volume{calls: per, source: sv.e.baseVolumeSource()}
}

// callers is how many of a helper's callers this call site is priced
// for. A helper with a fixed model answers for all of them. A helper
// whose model is a parameter answers for the callers the scanner
// tallied against its default string: the ones taking the default, and
// the ones writing that same string at the call, which the tally does
// not separate. The second group is a call site of its own as well, so
// that shape is billed twice until scan.CallerModel counts the two
// apart. Callers passing any other model are priced where they sit and
// stay out of this count. A resolution fan_in.multiply_when does not
// name counts as one, and so does a default no caller takes.
func (e *Engine) callers(site scan.Site) int {
	if !slices.Contains(e.Est.FanIn.MultiplyWhen, site.FanInStatus) {
		return 1
	}
	if len(site.CallerModels) == 0 {
		return site.FanIn
	}
	for _, cm := range site.CallerModels {
		if cm.Ref == site.Ref {
			return cm.Count
		}
	}
	return 1
}

// UnmatchedVolumeKeys names the volumes file keys no call site in this
// report used, so a stale path or a misspelled model is visible
// instead of silently priced as an estimate.
func (e *Engine) UnmatchedVolumeKeys(report *scan.Report, cat *catalog.Catalog) []string {
	return e.bindVolumes(report, cat).unmatched
}

func (e *Engine) baseVolumeSource() string {
	if e.DefaultVolumeSource == "" {
		return VolumeEstimate
	}
	return e.DefaultVolumeSource
}
