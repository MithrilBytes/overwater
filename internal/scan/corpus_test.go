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

func TestClassifierAccuracyOnCorpus(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "corpus", "labels.json"))
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
	if len(labels.Cases) < 10 {
		t.Fatalf("corpus has %d cases; it only means something with volume", len(labels.Cases))
	}

	report, err := Analyze(filepath.Join("..", "..", "corpus"), mustCatalog(t))
	if err != nil {
		t.Fatal(err)
	}
	byFile := map[string][]Site{}
	for _, site := range report.Sites {
		if site.Known {
			byFile[site.File] = append(byFile[site.File], site)
		}
	}

	correct := 0
	for _, c := range labels.Cases {
		sites := byFile[c.File]
		if len(sites) != 1 {
			t.Errorf("%s: %d known sites, want exactly 1", c.File, len(sites))
			continue
		}
		got := sites[0].Archetype
		if got == c.Archetype {
			correct++
		} else {
			t.Logf("miss: %s labeled %s, classified %s (%s confidence)",
				c.File, c.Archetype, got, sites[0].ArchetypeConfidence)
		}
	}
	accuracy := float64(correct) / float64(len(labels.Cases))
	t.Logf("classifier accuracy: %d/%d (%.2f)", correct, len(labels.Cases), accuracy)
	if accuracy < accuracyFloor {
		t.Fatalf("accuracy %.2f fell below the %.2f floor", accuracy, accuracyFloor)
	}
}
