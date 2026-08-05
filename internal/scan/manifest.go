package scan

import (
	"encoding/json"
	"path"
	"strings"
)

var npmSDKs = []string{
	"@ai-sdk/anthropic", "@ai-sdk/google", "@ai-sdk/openai",
	"@anthropic-ai/sdk", "@google/genai", "@google/generative-ai",
	"@langchain/anthropic", "@langchain/openai", "@mistralai/mistralai",
	"ai", "cohere-ai", "openai", "voyageai",
}

var pypiSDKs = []string{
	"anthropic", "cohere", "google-genai", "google-generativeai",
	"langchain-anthropic", "langchain-openai", "litellm", "mistralai",
	"openai", "voyageai",
}

var goSDKs = []string{
	"github.com/anthropics/anthropic-sdk-go",
	"github.com/openai/openai-go",
	"github.com/sashabaranov/go-openai",
	"google.golang.org/genai",
}

var gemSDKs = []string{"anthropic", "cohere", "openai", "ruby-openai"}

// scanManifest reads one file as a dependency manifest and returns the
// LLM SDKs it declares. Files that are not manifests return nothing.
func scanManifest(relPath string, data []byte) []SDK {
	switch path.Base(relPath) {
	case "package.json":
		return npmManifest(relPath, data)
	case "requirements.txt":
		return pipManifest(relPath, data)
	case "pyproject.toml":
		return pyprojectManifest(relPath, data)
	case "go.mod":
		return goManifest(relPath, data)
	case "Gemfile":
		return gemManifest(relPath, data)
	}
	return nil
}

func npmManifest(relPath string, data []byte) []SDK {
	var m struct {
		Dependencies    map[string]string `json:"dependencies"`
		DevDependencies map[string]string `json:"devDependencies"`
	}
	if err := json.Unmarshal(data, &m); err != nil {
		return nil
	}
	var sdks []SDK
	for _, name := range npmSDKs {
		_, inDeps := m.Dependencies[name]
		_, inDev := m.DevDependencies[name]
		if inDeps || inDev {
			sdks = append(sdks, SDK{Ecosystem: "npm", Name: name, File: relPath})
		}
	}
	return sdks
}

func pipManifest(relPath string, data []byte) []SDK {
	found := map[string]bool{}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.FieldsFunc(line, func(r rune) bool {
			return strings.ContainsRune("<>=!~[; ", r)
		})
		if len(fields) == 0 {
			continue
		}
		found[strings.ToLower(fields[0])] = true
	}
	var sdks []SDK
	for _, name := range pypiSDKs {
		if found[name] {
			sdks = append(sdks, SDK{Ecosystem: "pypi", Name: name, File: relPath})
		}
	}
	return sdks
}

// pyprojectManifest looks for quoted dependency names instead of parsing
// TOML. That is enough to establish SDK presence, which is all layer 1
// claims to do.
func pyprojectManifest(relPath string, data []byte) []SDK {
	s := string(data)
	var sdks []SDK
	for _, name := range pypiSDKs {
		if strings.Contains(s, `"`+name) || strings.Contains(s, `'`+name) {
			sdks = append(sdks, SDK{Ecosystem: "pypi", Name: name, File: relPath})
		}
	}
	return sdks
}

func goManifest(relPath string, data []byte) []SDK {
	s := string(data)
	var sdks []SDK
	for _, name := range goSDKs {
		if strings.Contains(s, name) {
			sdks = append(sdks, SDK{Ecosystem: "gomod", Name: name, File: relPath})
		}
	}
	return sdks
}

func gemManifest(relPath string, data []byte) []SDK {
	s := string(data)
	var sdks []SDK
	for _, name := range gemSDKs {
		if strings.Contains(s, `gem "`+name+`"`) || strings.Contains(s, `gem '`+name+`'`) {
			sdks = append(sdks, SDK{Ecosystem: "rubygems", Name: name, File: relPath})
		}
	}
	return sdks
}
