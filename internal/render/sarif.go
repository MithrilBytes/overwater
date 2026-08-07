package render

import (
	"encoding/json"
	"fmt"

	"github.com/MithrilBytes/overwater/rules"
)

const sarifSchema = "https://raw.githubusercontent.com/oasis-tcs/sarif-spec/master/Schemata/sarif-schema-2.1.0.json"

type sarifLog struct {
	Schema  string     `json:"$schema"`
	Version string     `json:"version"`
	Runs    []sarifRun `json:"runs"`
}

type sarifRun struct {
	Tool    sarifTool     `json:"tool"`
	Results []sarifResult `json:"results"`
}

type sarifTool struct {
	Driver sarifDriver `json:"driver"`
}

type sarifDriver struct {
	Name           string      `json:"name"`
	InformationURI string      `json:"informationUri"`
	Rules          []sarifRule `json:"rules"`
}

type sarifRule struct {
	ID               string       `json:"id"`
	ShortDescription sarifMessage `json:"shortDescription"`
}

type sarifMessage struct {
	Text string `json:"text"`
}

type sarifResult struct {
	RuleID    string          `json:"ruleId"`
	Level     string          `json:"level"`
	Message   sarifMessage    `json:"message"`
	Locations []sarifLocation `json:"locations"`
}

type sarifLocation struct {
	PhysicalLocation sarifPhysicalLocation `json:"physicalLocation"`
}

type sarifPhysicalLocation struct {
	ArtifactLocation sarifArtifactLocation `json:"artifactLocation"`
	Region           sarifRegion           `json:"region"`
}

type sarifArtifactLocation struct {
	URI string `json:"uri"`
}

type sarifRegion struct {
	StartLine int `json:"startLine"`
}

// SARIF renders the findings as a SARIF 2.1.0 log with a single run for
// code scanning surfaces. The catalog version in meta is a price list
// date, not a tool release, so the driver carries no semanticVersion.
func SARIF(findings []rules.Finding, meta Meta) ([]byte, error) {
	driver := sarifDriver{
		Name:           "overwater",
		InformationURI: "https://github.com/MithrilBytes/overwater",
		Rules:          make([]sarifRule, 0),
	}
	seen := make(map[string]bool)
	results := make([]sarifResult, 0, len(findings))
	for _, f := range findings {
		if !seen[f.RuleID] {
			seen[f.RuleID] = true
			driver.Rules = append(driver.Rules, sarifRule{
				ID:               f.RuleID,
				ShortDescription: sarifMessage{Text: fmt.Sprintf("Overwater rule %s.", f.RuleID)},
			})
		}
		results = append(results, sarifResult{
			RuleID: f.RuleID,
			Level:  sarifLevel(f.Confidence),
			Message: sarifMessage{Text: fmt.Sprintf(
				"Current: %s. Candidate: %s. Tripwire: %s.",
				current(f), f.CandidateText, f.Tripwire)},
			Locations: []sarifLocation{{
				PhysicalLocation: sarifPhysicalLocation{
					ArtifactLocation: sarifArtifactLocation{URI: f.File},
					Region:           sarifRegion{StartLine: f.Line},
				},
			}},
		})
	}
	doc := sarifLog{
		Schema:  sarifSchema,
		Version: "2.1.0",
		Runs:    []sarifRun{{Tool: sarifTool{Driver: driver}, Results: results}},
	}
	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(out, '\n'), nil
}

// sarifLevel maps finding confidence onto the SARIF level vocabulary:
// high becomes warning, medium and low become note.
func sarifLevel(confidence string) string {
	if confidence == "high" {
		return "warning"
	}
	return "note"
}
