// Package docs regenerates the parts of README.md and site/index.html
// that are facts about this repository rather than prose about it.
//
// Every one of them has gone stale at least once. The rules list said
// twelve while fifteen shipped. Both install examples sat on v2.6.0
// after v2.7.0 was out, and the site's looked current to anyone whose
// browser reached the releases API, which is why nobody noticed.
//
// Two things are deliberately NOT generated.
//
// A count inside wrapped prose is not, because README wraps at 72
// columns and rewriting a number can move the wrap, so a generator that
// touched it would reflow paragraphs nobody asked it to touch. Those
// counts are held by tests instead: a failure asks a person to edit the
// sentence, which is the right amount of friction for a number that
// changes once a quarter.
//
// A number inside a release note is not, at any cost. "v2.0 added
// twelve rules and 88 catalog entries" is a record of what shipped that
// day and is still true; only a generator that could not tell history
// from status would update it.
package docs

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Facts are what the repository knows about itself.
type Facts struct {
	Version        string   // the release action.yml names
	Rules          []string // rule ids, sorted
	CatalogModels  int
	CatalogVenders int
}

var (
	actionVersionRe = regexp.MustCompile(`version="(v[0-9]+\.[0-9]+\.[0-9]+)"`)
	installRe       = regexp.MustCompile(`sh install\.sh v[0-9]+\.[0-9]+\.[0-9]+`)
	bakedVersionRe  = regexp.MustCompile(`(data-version>)v[0-9]+\.[0-9]+\.[0-9]+(<)`)
	rulesSectionRe  = regexp.MustCompile(`(?s)(### Rules\n\n).*?(\n\n## )`)
)

// Read gathers the facts from the repository rooted at dir.
func Read(dir string) (Facts, error) {
	var f Facts

	action, err := os.ReadFile(filepath.Join(dir, "action.yml"))
	if err != nil {
		return f, err
	}
	m := actionVersionRe.FindStringSubmatch(string(action))
	if m == nil {
		return f, fmt.Errorf(`action.yml has no version="vX.Y.Z"`)
	}
	f.Version = m[1]

	entries, err := os.ReadDir(filepath.Join(dir, "rules"))
	if err != nil {
		return f, err
	}
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".yaml") || name == "estimates.yaml" {
			continue // estimates.yaml holds cost assumptions, not a rule
		}
		f.Rules = append(f.Rules, strings.TrimSuffix(name, ".yaml"))
	}
	sort.Strings(f.Rules)
	if len(f.Rules) == 0 {
		return f, fmt.Errorf("found no rule files under %s/rules", dir)
	}

	raw, err := os.ReadFile(filepath.Join(dir, "catalog", "catalog.json"))
	if err != nil {
		return f, err
	}
	var cat struct {
		Models []struct {
			Provider string `json:"provider"`
		} `json:"models"`
	}
	if err := json.Unmarshal(raw, &cat); err != nil {
		return f, err
	}
	providers := map[string]bool{}
	for _, m := range cat.Models {
		providers[m.Provider] = true
	}
	f.CatalogModels, f.CatalogVenders = len(cat.Models), len(providers)
	return f, nil
}

// README rewrites the generated parts of the README and returns it.
func README(src string, f Facts) (string, error) {
	if !installRe.MatchString(src) {
		return "", fmt.Errorf("README has no install.sh example to update")
	}
	out := installRe.ReplaceAllString(src, "sh install.sh "+f.Version)

	if !rulesSectionRe.MatchString(out) {
		return "", fmt.Errorf("README has no ### Rules section to update")
	}
	out = rulesSectionRe.ReplaceAllString(out, "${1}"+wrap(f.Rules, 72)+"${2}")
	return out, nil
}

// Site rewrites the generated parts of the project page.
//
// The baked version is what a reader sees when the page's own fetch of
// the releases API fails or scripts are off. It updating itself in the
// common case is exactly why it drifted unnoticed.
func Site(src string, f Facts) (string, error) {
	if !bakedVersionRe.MatchString(src) {
		return "", fmt.Errorf("site has no [data-version] value to update")
	}
	return bakedVersionRe.ReplaceAllString(src, "${1}"+f.Version+"${2}"), nil
}

// wrap renders ids as a backticked, comma separated sentence wrapped at
// width, matching how the section was written by hand.
func wrap(ids []string, width int) string {
	var lines []string
	line := ""
	for i, id := range ids {
		piece := "`" + id + "`"
		if i < len(ids)-1 {
			piece += ","
		} else {
			piece += "."
		}
		switch {
		case line == "":
			line = piece
		case len(line)+1+len(piece) <= width:
			line += " " + piece
		default:
			lines = append(lines, line)
			line = piece
		}
	}
	return strings.Join(append(lines, line), "\n")
}
