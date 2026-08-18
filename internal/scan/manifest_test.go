package scan

import "testing"

func sdkNames(sdks []SDK) []string {
	var names []string
	for _, s := range sdks {
		names = append(names, s.Name)
	}
	return names
}

func hasName(sdks []SDK, name string) bool {
	for _, s := range sdks {
		if s.Name == name {
			return true
		}
	}
	return false
}

func TestManifestNpmGatewaysAndAgents(t *testing.T) {
	content := `{"dependencies": {
		"@ai-sdk/amazon-bedrock": "^1.0.0",
		"@ai-sdk/google-vertex": "^2.0.0",
		"@openrouter/ai-sdk-provider": "^0.4.0",
		"@openai/agents": "^0.1.0"
	}}`
	sdks := scanManifest("package.json", content)
	for _, want := range []string{"@ai-sdk/amazon-bedrock", "@ai-sdk/google-vertex", "@openrouter/ai-sdk-provider", "@openai/agents"} {
		if !hasName(sdks, want) {
			t.Errorf("got %v, want %s", sdkNames(sdks), want)
		}
	}
}

func TestManifestPypiGatewaysAndAgents(t *testing.T) {
	content := "google-cloud-aiplatform==1.60.0\nlangchain-aws>=0.2\nlangchain-google-genai\ninstructor\npydantic-ai\nopenai-agents\n"
	sdks := scanManifest("requirements.txt", content)
	for _, want := range []string{"google-cloud-aiplatform", "langchain-aws", "langchain-google-genai", "instructor", "pydantic-ai", "openai-agents"} {
		if !hasName(sdks, want) {
			t.Errorf("got %v, want %s", sdkNames(sdks), want)
		}
	}
}

func TestManifestGoBedrock(t *testing.T) {
	content := "module example.com/app\n\nrequire github.com/aws/aws-sdk-go-v2/service/bedrockruntime v1.20.0\n"
	sdks := scanManifest("go.mod", content)
	if len(sdks) != 1 || sdks[0].Name != "github.com/aws/aws-sdk-go-v2/service/bedrockruntime" {
		t.Fatalf("got %v, want the Bedrock runtime client", sdkNames(sdks))
	}
}

func TestSDKsWithoutSitesReportsTheSilentTree(t *testing.T) {
	r := &Report{
		SDKs: []SDK{
			{Ecosystem: "npm", Name: "@ai-sdk/amazon-bedrock", File: "services/ingest/package.json"},
			{Ecosystem: "npm", Name: "openai", File: "services/chat/package.json"},
		},
		Sites: []Site{{File: "services/chat/route.ts", Line: 7}},
	}
	missed := r.SDKsWithoutSites()
	if len(missed) != 1 || missed[0].File != "services/ingest/package.json" {
		t.Fatalf("got %+v, want only the tree that resolved nothing", missed)
	}
}

func TestSDKsWithoutSitesScopesRootManifestToTheWholeRepo(t *testing.T) {
	r := &Report{
		SDKs:  []SDK{{Ecosystem: "pypi", Name: "anthropic", File: "requirements.txt"}},
		Sites: []Site{{File: "svc/extract.py", Line: 24}},
	}
	if missed := r.SDKsWithoutSites(); len(missed) != 0 {
		t.Fatalf("got %+v, want nothing: a root manifest owns every file", missed)
	}
}

func TestSDKsWithoutSitesIgnoresASiblingTree(t *testing.T) {
	// services/ingest must not be excused by services/ingestion, which
	// shares its prefix but not its tree.
	r := &Report{
		SDKs:  []SDK{{Ecosystem: "npm", Name: "openai", File: "services/ingest/package.json"}},
		Sites: []Site{{File: "services/ingestion/route.ts", Line: 3}},
	}
	if missed := r.SDKsWithoutSites(); len(missed) != 1 {
		t.Fatalf("got %+v, want the ingest manifest reported", missed)
	}
}
