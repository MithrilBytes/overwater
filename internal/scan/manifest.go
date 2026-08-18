package scan

import (
	"encoding/json"
	"path"
	"strings"
)

// The hosted gateways (Bedrock, Vertex, OpenRouter) and the agent
// frameworks belong here as much as the first party clients do: a repo
// that reaches a frontier model through one of them spends the same
// money, and layer 1 is the only layer that sees it before a call site
// resolves.
var npmSDKs = []string{
	"@ai-sdk/amazon-bedrock", "@ai-sdk/anthropic", "@ai-sdk/google",
	"@ai-sdk/google-vertex", "@ai-sdk/openai", "@anthropic-ai/sdk",
	"@google/genai", "@google/generative-ai", "@langchain/anthropic",
	"@langchain/openai", "@mistralai/mistralai", "@openai/agents",
	"@openrouter/ai-sdk-provider", "ai", "cohere-ai", "openai",
	"voyageai",
}

var pypiSDKs = []string{
	"anthropic", "cohere", "google-cloud-aiplatform", "google-genai",
	"google-generativeai", "instructor", "langchain-anthropic",
	"langchain-aws", "langchain-google-genai", "langchain-openai",
	"litellm", "mistralai", "openai", "openai-agents", "pydantic-ai",
	"voyageai",
}

var goSDKs = []string{
	"github.com/anthropics/anthropic-sdk-go",
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime",
	"github.com/openai/openai-go",
	"github.com/sashabaranov/go-openai",
	"google.golang.org/genai",
}

var gemSDKs = []string{"anthropic", "cohere", "openai", "ruby-openai"}

// Substring markers for the manifest formats where presence detection
// is the whole job.
var javaSDKs = []string{"com.anthropic", "com.openai", "dev.langchain4j", "com.theokanning"}
var csprojSDKs = []string{"Anthropic.SDK", "OpenAI", "Azure.AI.OpenAI", "Microsoft.SemanticKernel"}
var composerSDKs = []string{"openai-php", "anthropic-ai"}
var cargoSDKs = []string{"async-openai", "anthropic", "genai"}
var swiftSDKs = []string{"openai", "anthropic", "generative-ai-swift"}

// scanManifest reads one file as a dependency manifest and returns the
// LLM SDKs it declares. Files that are not manifests return nothing.
func scanManifest(relPath, data string) []SDK {
	base := path.Base(relPath)
	if strings.HasSuffix(base, ".csproj") {
		return substringManifest(relPath, data, "nuget", csprojSDKs, false)
	}
	switch base {
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
	case "pom.xml", "build.gradle", "build.gradle.kts":
		return substringManifest(relPath, data, "maven", javaSDKs, false)
	case "composer.json":
		return substringManifest(relPath, data, "composer", composerSDKs, false)
	case "Cargo.toml":
		return substringManifest(relPath, data, "cargo", cargoSDKs, false)
	case "Package.swift":
		return substringManifest(relPath, data, "swiftpm", swiftSDKs, true)
	}
	return nil
}

// SDKsWithoutSites names the SDKs declared in a manifest whose tree
// resolved no model reference. Layer 1 seeing a dependency while layers
// 2 through 4 see nothing is the shape of a miss, not of a repo that
// makes no calls, and saying so is the same contract as an unpriced
// call: silence would read as a clean bill of health.
func (r *Report) SDKsWithoutSites() []SDK {
	var out []SDK
	for _, sdk := range r.SDKs {
		if !hasSiteUnder(r.Sites, path.Dir(sdk.File)) {
			out = append(out, sdk)
		}
	}
	return out
}

// hasSiteUnder scopes the question to the manifest's own tree, so in a
// monorepo a package that resolved nothing is not excused by a sibling
// that did. A root manifest owns every file.
func hasSiteUnder(sites []Site, dir string) bool {
	if dir == "." {
		return len(sites) > 0
	}
	for _, s := range sites {
		if strings.HasPrefix(s.File, dir+"/") {
			return true
		}
	}
	return false
}

func substringManifest(relPath, data, ecosystem string, names []string, fold bool) []SDK {
	s := data
	if fold {
		s = strings.ToLower(s)
	}
	var sdks []SDK
	for _, name := range names {
		probe := name
		if fold {
			probe = strings.ToLower(name)
		}
		if strings.Contains(s, probe) {
			sdks = append(sdks, SDK{Ecosystem: ecosystem, Name: name, File: relPath})
		}
	}
	return sdks
}

func npmManifest(relPath, data string) []SDK {
	var m struct {
		Dependencies    map[string]string `json:"dependencies"`
		DevDependencies map[string]string `json:"devDependencies"`
	}
	if err := json.Unmarshal([]byte(data), &m); err != nil {
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

func pipManifest(relPath, data string) []SDK {
	found := map[string]bool{}
	for _, line := range strings.Split(data, "\n") {
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
func pyprojectManifest(relPath, data string) []SDK {
	var sdks []SDK
	for _, name := range pypiSDKs {
		if strings.Contains(data, `"`+name) || strings.Contains(data, `'`+name) {
			sdks = append(sdks, SDK{Ecosystem: "pypi", Name: name, File: relPath})
		}
	}
	return sdks
}

func goManifest(relPath, data string) []SDK {
	var sdks []SDK
	for _, name := range goSDKs {
		if strings.Contains(data, name) {
			sdks = append(sdks, SDK{Ecosystem: "gomod", Name: name, File: relPath})
		}
	}
	return sdks
}

func gemManifest(relPath, data string) []SDK {
	var sdks []SDK
	for _, name := range gemSDKs {
		if strings.Contains(data, `gem "`+name+`"`) || strings.Contains(data, `gem '`+name+`'`) {
			sdks = append(sdks, SDK{Ecosystem: "rubygems", Name: name, File: relPath})
		}
	}
	return sdks
}
