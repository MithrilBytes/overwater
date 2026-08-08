package scan

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

// The corpus is split in labels.json: tune cases are what a change may
// be developed against, holdout cases only measure it. These floors are
// a ratchet. Raise them when the classifier earns it; never lower one to
// make a change fit.
const (
	holdoutFloor = 0.95
	tuneFloor    = 0.95
)

// Per class floors on the holdout, so a regression in one archetype
// cannot hide behind a good total. Each is today's score less about one
// case; embedding is exact, since an embedding endpoint is read off the
// call rather than scored.
var classFloors = map[string]struct{ precision, recall float64 }{
	ArchetypeAgentic:        {0.80, 0.80},
	ArchetypeChat:           {0.80, 0.80},
	ArchetypeClassification: {0.85, 0.85},
	ArchetypeCodegen:        {0.80, 0.80},
	ArchetypeEmbedding:      {1.00, 1.00},
	ArchetypeExtraction:     {0.85, 0.85},
	ArchetypeModeration:     {0.80, 0.80},
	ArchetypeReranking:      {0.80, 0.80},
	ArchetypeSummarization:  {0.85, 0.85},
	ArchetypeTranscription:  {0.80, 0.80},
	ArchetypeTranslation:    {0.80, 0.80},
	ArchetypeVision:         {0.80, 0.80},
}

type corpusCase struct {
	File      string `json:"file"`
	Archetype string `json:"archetype"`
	Split     string `json:"split"`
}

type tally struct{ tp, fp, fn int }

func (t tally) precision() float64 {
	if t.tp+t.fp == 0 {
		return 1
	}
	return float64(t.tp) / float64(t.tp+t.fp)
}

func (t tally) recall() float64 {
	if t.tp+t.fn == 0 {
		return 1
	}
	return float64(t.tp) / float64(t.tp+t.fn)
}

func TestCorpusAccuracy(t *testing.T) {
	cases := loadCorpus(t)
	if len(cases) < 250 {
		t.Fatalf("corpus has %d cases; the floors are calibrated for at least 250", len(cases))
	}

	report, err := Analyze(filepath.Join("..", "..", "corpus", "testdata"), mustCatalog(t))
	if err != nil {
		t.Fatal(err)
	}
	byFile := map[string][]Site{}
	for _, site := range report.Sites {
		if site.Known && site.ViaConfig == "" {
			byFile[site.File] = append(byFile[site.File], site)
		}
	}

	perSplit := map[string]*tally{"tune": {}, "holdout": {}}
	perClass := map[string]map[string]*tally{"tune": {}, "holdout": {}}
	confTotal := map[string]int{}
	confRight := map[string]int{}
	get := func(split, arch string) *tally {
		if perClass[split][arch] == nil {
			perClass[split][arch] = &tally{}
		}
		return perClass[split][arch]
	}

	for _, c := range cases {
		sites := byFile[c.File]
		if len(sites) != 1 {
			t.Errorf("%s: %d known sites, want exactly 1", c.File, len(sites))
			continue
		}
		got := sites[0].Archetype
		confTotal[sites[0].ArchetypeConfidence]++
		if got == c.Archetype {
			perSplit[c.Split].tp++
			get(c.Split, c.Archetype).tp++
			confRight[sites[0].ArchetypeConfidence]++
			continue
		}
		perSplit[c.Split].fp++
		get(c.Split, c.Archetype).fn++
		get(c.Split, got).fp++
		// Reading holdout misses turns the holdout into a second tune set,
		// so they stay behind an env var a human sets deliberately.
		if c.Split == "tune" || os.Getenv("OVERWATER_SHOW_HOLDOUT") != "" {
			t.Logf("%s miss: %s labeled %s, classified %s (%s confidence)",
				c.Split, c.File, c.Archetype, got, sites[0].ArchetypeConfidence)
		}
	}

	for _, split := range []string{"tune", "holdout"} {
		m := perSplit[split]
		n := m.tp + m.fp
		t.Logf("%s accuracy: %d/%d (%.2f)", split, m.tp, n, float64(m.tp)/float64(n))
		for _, arch := range sortedKeys(perClass[split]) {
			c := perClass[split][arch]
			t.Logf("  %-8s %-15s precision %.2f recall %.2f (tp %d fp %d fn %d)",
				split, arch, c.precision(), c.recall(), c.tp, c.fp, c.fn)
		}
	}
	for _, conf := range []string{"high", "medium", "low"} {
		if confTotal[conf] > 0 {
			t.Logf("confidence %-6s accuracy %d/%d (%.2f)", conf, confRight[conf], confTotal[conf],
				float64(confRight[conf])/float64(confTotal[conf]))
		}
	}
	// Calibration: high confidence answers must not be less accurate
	// than low confidence ones, once both buckets have volume.
	if confTotal["high"] >= 5 && confTotal["low"] >= 5 {
		high := float64(confRight["high"]) / float64(confTotal["high"])
		low := float64(confRight["low"]) / float64(confTotal["low"])
		if high < low {
			t.Errorf("confidence is miscalibrated: high %.2f below low %.2f", high, low)
		}
	}

	for _, arch := range sortedKeys(classFloors) {
		floor := classFloors[arch]
		m := perClass["holdout"][arch]
		if m == nil {
			t.Errorf("holdout has no %s cases; the class floor cannot be measured", arch)
			continue
		}
		if p := m.precision(); p < floor.precision {
			t.Errorf("holdout %s precision %.2f below the %.2f floor", arch, p, floor.precision)
		}
		if r := m.recall(); r < floor.recall {
			t.Errorf("holdout %s recall %.2f below the %.2f floor", arch, r, floor.recall)
		}
	}
	check := func(split string, floor float64) {
		m := perSplit[split]
		if got := float64(m.tp) / float64(m.tp+m.fp); got < floor {
			t.Errorf("%s accuracy %.2f fell below the %.2f floor", split, got, floor)
		}
	}
	check("holdout", holdoutFloor)
	check("tune", tuneFloor)
}

func loadCorpus(t *testing.T) []corpusCase {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "corpus", "testdata", "labels.json"))
	if err != nil {
		t.Fatal(err)
	}
	var labels struct {
		Cases []corpusCase `json:"cases"`
	}
	if err := json.Unmarshal(raw, &labels); err != nil {
		t.Fatal(err)
	}
	holdout := 0
	for _, c := range labels.Cases {
		switch c.Split {
		case "tune":
		case "holdout":
			holdout++
		default:
			t.Fatalf("%s: split %q, want tune or holdout", c.File, c.Split)
		}
		if !validArchetype(c.Archetype) && c.Archetype != ArchetypeUnknown {
			t.Fatalf("%s: unknown archetype %q", c.File, c.Archetype)
		}
	}
	if share := float64(holdout) / float64(len(labels.Cases)); share < 0.25 || share > 0.35 {
		t.Fatalf("holdout is %.0f%% of the corpus, want roughly 30%%", share*100)
	}
	return labels.Cases
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
