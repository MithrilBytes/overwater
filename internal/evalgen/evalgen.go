// Package evalgen writes the runnable A/B eval scripts. The scripts are
// the one deliberate exception to the scanner's no network rule: they
// call model APIs, but only when the user runs them, with the user's
// own keys, outside the scanner. Nothing in this package executes
// anything.
package evalgen

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/MithrilBytes/overwater/catalog"
	"github.com/MithrilBytes/overwater/rules"
)

// Generate writes one script per finding that nominates a different
// model, and reports what it skipped and why.
func Generate(findings []rules.Finding, cat *catalog.Catalog, dir string) (written, skipped []string, err error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, nil, err
	}
	for _, f := range findings {
		if f.CandidateModel == "" {
			skipped = append(skipped, fmt.Sprintf("%s at %s:%d: candidate is not a different model", f.RuleID, f.File, f.Line))
			continue
		}
		tpl, reason := pickTemplate(cat.ByName(f.Model))
		if tpl == "" {
			skipped = append(skipped, fmt.Sprintf("%s at %s:%d: %s", f.RuleID, f.File, f.Line, reason))
			continue
		}
		name := scriptName(f)
		body := fill(tpl, f, name)
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
			return written, skipped, err
		}
		written = append(written, path)
	}
	return written, skipped, nil
}

func pickTemplate(m *catalog.Model) (string, string) {
	if m == nil {
		return "", "model is not in the catalog"
	}
	if m.Tier == "embedding" {
		if m.Provider == "openai" {
			return embeddingTemplate, ""
		}
		return "", "no embedding eval template for provider " + m.Provider + " yet"
	}
	switch m.Provider {
	case "anthropic":
		return anthropicTemplate, ""
	case "openai":
		return openaiTemplate, ""
	}
	return "", "no eval template for provider " + m.Provider + " yet"
}

func scriptName(f rules.Finding) string {
	slug := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			return r
		case r >= 'A' && r <= 'Z':
			return r + ('a' - 'A')
		default:
			return '_'
		}
	}, f.File)
	return fmt.Sprintf("eval_%s_%s_%d.py", f.RuleID, slug, f.Line)
}

func fill(tpl string, f rules.Finding, name string) string {
	r := strings.NewReplacer(
		"{{CURRENT}}", f.Model,
		"{{CANDIDATE}}", f.CandidateModel,
		"{{RULE}}", f.RuleID,
		"{{SITE}}", fmt.Sprintf("%s:%d", f.File, f.Line),
		"{{TRIPWIRE}}", f.Tripwire,
		"{{SCRIPT}}", name,
	)
	return r.Replace(tpl)
}
