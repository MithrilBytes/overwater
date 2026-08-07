package scan

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// accuracyFloor is the classifier's ratchet: measured accuracy on the
// labeled corpus must not fall below it. Raise it when the classifier
// earns it; never lower it to make a change fit.
const accuracyFloor = 0.85

func TestCorpusAccuracy(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "corpus", "testdata", "labels.json"))
	if err != nil {
		t.Fatal(err)
	}
	var labels struct {
		Cases []struct {
			File      string `json:"file"`
			Archetype string `json:"archetype"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(raw, &labels); err != nil {
		t.Fatal(err)
	}
	if len(labels.Cases) < 100 {
		t.Fatalf("corpus has %d cases; the floor is calibrated for at least 100", len(labels.Cases))
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

	type tally struct{ tp, fp, fn int }
	perArchetype := map[string]*tally{}
	get := func(a string) *tally {
		if perArchetype[a] == nil {
			perArchetype[a] = &tally{}
		}
		return perArchetype[a]
	}
	confTotal := map[string]int{}
	confRight := map[string]int{}

	correct := 0
	for _, c := range labels.Cases {
		sites := byFile[c.File]
		if len(sites) != 1 {
			t.Errorf("%s: %d known sites, want exactly 1", c.File, len(sites))
			continue
		}
		got := sites[0].Archetype
		confTotal[sites[0].ArchetypeConfidence]++
		if got == c.Archetype {
			correct++
			get(c.Archetype).tp++
			confRight[sites[0].ArchetypeConfidence]++
		} else {
			get(c.Archetype).fn++
			get(got).fp++
			t.Logf("miss: %s labeled %s, classified %s (%s confidence)",
				c.File, c.Archetype, got, sites[0].ArchetypeConfidence)
		}
	}

	accuracy := float64(correct) / float64(len(labels.Cases))
	t.Logf("classifier accuracy: %d/%d (%.2f)", correct, len(labels.Cases), accuracy)
	for a, m := range perArchetype {
		precision, recall := 1.0, 1.0
		if m.tp+m.fp > 0 {
			precision = float64(m.tp) / float64(m.tp+m.fp)
		}
		if m.tp+m.fn > 0 {
			recall = float64(m.tp) / float64(m.tp+m.fn)
		}
		t.Logf("%-15s precision %.2f recall %.2f (tp %d fp %d fn %d)", a, precision, recall, m.tp, m.fp, m.fn)
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
	if accuracy < accuracyFloor {
		t.Fatalf("accuracy %.2f fell below the %.2f floor", accuracy, accuracyFloor)
	}
}
