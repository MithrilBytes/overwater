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
	"strconv"
	"strings"

	"github.com/MithrilBytes/overwater/catalog"
	"github.com/MithrilBytes/overwater/rules"
)

// openAICompat lists the providers whose chat endpoint speaks the
// OpenAI protocol. They share one template; only the base URL and the
// key environment variable differ.
var openAICompat = map[string]struct {
	BaseURL string
	EnvVar  string
}{
	"deepseek": {"https://api.deepseek.com", "DEEPSEEK_API_KEY"},
	"xai":      {"https://api.x.ai/v1", "XAI_API_KEY"},
	"groq":     {"https://api.groq.com/openai/v1", "GROQ_API_KEY"},
	"mistral":  {"https://api.mistral.ai/v1", "MISTRAL_API_KEY"},
}

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
		current := cat.ByName(f.Model)
		tpl, reason := pickTemplate(current)
		if tpl == "" {
			skipped = append(skipped, fmt.Sprintf("%s at %s:%d: %s", f.RuleID, f.File, f.Line, reason))
			continue
		}
		name := scriptName(f)
		body := fill(tpl, f, name, current, cat.ByName(f.CandidateModel))
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
	if _, ok := openAICompat[m.Provider]; ok {
		return compatTemplate, ""
	}
	switch m.Provider {
	case "anthropic":
		return anthropicTemplate, ""
	case "openai":
		return openaiTemplate, ""
	case "google":
		return googleTemplate, ""
	case "cohere":
		return cohereTemplate, ""
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

// fill substitutes the placeholders. Prices are baked into the script
// so it can estimate spend without a network fetch; a candidate the
// catalog does not know prices as zero rather than failing the write.
func fill(tpl string, f rules.Finding, name string, current, candidate *catalog.Model) string {
	compat := openAICompat[current.Provider]
	var candIn, candOut float64
	if candidate != nil {
		candIn, candOut = candidate.InputPerMtok, candidate.OutputPerMtok
	}
	r := strings.NewReplacer(
		"{{CURRENT}}", f.Model,
		"{{CANDIDATE}}", f.CandidateModel,
		"{{RULE}}", f.RuleID,
		"{{SITE}}", fmt.Sprintf("%s:%d", f.File, f.Line),
		"{{TRIPWIRE}}", f.Tripwire,
		"{{SCRIPT}}", name,
		"{{PROVIDER}}", current.Provider,
		"{{BASE_URL}}", compat.BaseURL,
		"{{ENV_VAR}}", compat.EnvVar,
		"{{CURRENT_IN}}", dollars(current.InputPerMtok),
		"{{CURRENT_OUT}}", dollars(current.OutputPerMtok),
		"{{CANDIDATE_IN}}", dollars(candIn),
		"{{CANDIDATE_OUT}}", dollars(candOut),
	)
	return r.Replace(tpl)
}

// dollars renders a per million token price as a plain Python number.
func dollars(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64)
}
