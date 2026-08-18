package evalgen

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MithrilBytes/overwater/internal/scan"
	"github.com/MithrilBytes/overwater/rules"
)

// The generated scripts are run here against stub SDKs, with no key and
// no network, because the exit code is the product: a person puts one
// of these in CI and reads the code, not the report.

// stubAnthropic answers "yes" for the current model and whatever the
// environment says for the candidate, which is how a run is made to
// agree, disagree, or fail outright.
const stubAnthropic = `import os


class _Block:
    def __init__(self, text):
        self.text = text


class _Usage:
    input_tokens = 3
    output_tokens = 4


class _Message:
    def __init__(self, text):
        self.content = [_Block(text)]
        self.usage = _Usage()


class _Messages:
    def create(self, **kwargs):
        answer = os.environ.get("STUB_CANDIDATE", "yes")
        if answer == "raise":
            raise RuntimeError("the provider is down")
        return _Message({"anthropic-big": "yes"}.get(kwargs["model"], answer))


class Anthropic:
    def __init__(self):
        self.messages = _Messages()
`

// stubShapeEcho prints the shape of the call it was handed, so a run
// can be checked against the call site the eval was generated for.
const stubShapeEcho = `class _Block:
    def __init__(self, text):
        self.text = text


class _Usage:
    input_tokens = 3
    output_tokens = 4


class _Message:
    def __init__(self, text):
        self.content = [_Block(text)]
        self.usage = _Usage()


class _Messages:
    def create(self, **kwargs):
        print("shape: max_tokens=%s tools=%s parts=%s"
              % (kwargs["max_tokens"], "tools" in kwargs,
                 [part["type"] for part in kwargs["messages"][0]["content"]]))
        return _Message("yes")


class Anthropic:
    def __init__(self):
        self.messages = _Messages()
`

// stubOpenAI embeds each text as a point on the unit circle, paired off
// so every text has one obvious nearest neighbor. STUB_FLIP repairs
// them for the candidate model, which moves every neighbor.
const stubOpenAI = `import math
import os

PAIRED = [0.0, 0.05, 1.0, 1.05]
REPAIRED = [0.0, 1.0, 0.05, 1.05]


class _Item:
    def __init__(self, embedding):
        self.embedding = embedding


class _Response:
    def __init__(self, data):
        self.data = data


class _Embeddings:
    def create(self, model, input):
        angles = PAIRED
        if model == "embed-small" and os.environ.get("STUB_FLIP") == "1":
            angles = REPAIRED
        data = []
        for i in range(len(input)):
            a = angles[i % len(angles)]
            data.append(_Item([math.cos(a), math.sin(a)]))
        return _Response(data)


class OpenAI:
    def __init__(self):
        self.embeddings = _Embeddings()
`

type scriptRun struct {
	finding rules.Finding
	stub    string // module source, named for the SDK the template imports
	stubAs  string
	prompts string // prompts.jsonl contents
	arg     string // the path argument, relative to the script directory
	env     string // one NAME=value for the stub
}

// run generates the script, drops the stub beside it, and returns the
// exit code with the combined output.
func (r scriptRun) run(t *testing.T) (int, string) {
	t.Helper()
	py, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 not on PATH")
	}
	dir := t.TempDir()
	written, skipped, err := Generate([]rules.Finding{r.finding}, testCatalog(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(written) != 1 {
		t.Fatalf("written = %v, skipped = %v, want one script", written, skipped)
	}
	// Python puts the script's own directory first on the import path,
	// so the stub beside it wins over any installed SDK.
	if err := os.WriteFile(filepath.Join(dir, r.stubAs), []byte(r.stub), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "prompts.jsonl"), []byte(r.prompts), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(py, written[0], filepath.Join(dir, r.arg))
	cmd.Env = append(os.Environ(), r.env)
	out, err := cmd.CombinedOutput()
	code := 0
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		code = exit.ExitCode()
	} else if err != nil {
		t.Fatal(err)
	}
	return code, string(out)
}

const chatPrompts = `{"prompt": "one"}
{"prompt": "two"}
{"prompt": "three"}
`

const embeddingPrompts = `{"prompt": "one"}
{"prompt": "two"}
{"prompt": "three"}
{"prompt": "four"}
`

// The exit code is the answer a CI job reads: 0 the tripwire held, 1 it
// tripped, 2 the run could not answer. Nothing but a tripped tripwire
// may return 1.
func TestChatScriptExitsOnTripwire(t *testing.T) {
	agreement := rules.TripwireCheck{Metric: "agreement", Compare: "below", Threshold: 97}
	cases := []struct {
		name     string
		check    rules.TripwireCheck
		stubEnv  string
		arg      string
		wantCode int
		wantOut  string
	}{
		{
			name:     "agreement above the bar",
			check:    agreement,
			stubEnv:  "yes",
			arg:      "prompts.jsonl",
			wantCode: 0,
			wantOut:  "agreement is 100.0%, trips below 97%: held",
		},
		{
			name:     "agreement under the bar",
			check:    agreement,
			stubEnv:  "no",
			arg:      "prompts.jsonl",
			wantCode: 1,
			wantOut:  "agreement is 0.0%, trips below 97%: tripped, keep the current model",
		},
		{
			name:     "no machine readable form to gate on",
			stubEnv:  "no",
			arg:      "prompts.jsonl",
			wantCode: 0,
			wantOut:  "tripwire: If eval agreement drops below 97%, stay put.",
		},
		{
			name:     "prompts file missing",
			check:    agreement,
			stubEnv:  "yes",
			arg:      "absent.jsonl",
			wantCode: 2,
			wantOut:  "cannot read",
		},
		{
			name:     "the provider fails mid run",
			check:    agreement,
			stubEnv:  "raise",
			arg:      "prompts.jsonl",
			wantCode: 2,
			wantOut:  "the run failed before it could answer",
		},
		{
			name:     "metric this script does not measure",
			check:    rules.TripwireCheck{Metric: "nearest_neighbor_agreement", Compare: "below", Threshold: 90},
			stubEnv:  "yes",
			arg:      "prompts.jsonl",
			wantCode: 2,
			wantOut:  "does not measure nearest_neighbor_agreement",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := chatFinding("anthropic")
			f.TripwireCheck = tc.check
			code, out := scriptRun{
				finding: f,
				stub:    stubAnthropic,
				stubAs:  "anthropic.py",
				prompts: chatPrompts,
				arg:     tc.arg,
				env:     "STUB_CANDIDATE=" + tc.stubEnv,
			}.run(t)
			if code != tc.wantCode {
				t.Errorf("exit = %d, want %d\n%s", code, tc.wantCode, out)
			}
			if !strings.Contains(out, tc.wantOut) {
				t.Errorf("output is missing %q\n%s", tc.wantOut, out)
			}
		})
	}
}

// The eval only exercises the call site if the row's shape reaches the
// API: the cap the site set, the tools it passed, the image it sent.
func TestChatScriptSendsTheRowShape(t *testing.T) {
	prompts := `{"prompt": "one", "max_tokens": 1024,` +
		` "image_url": "data:image/png;base64,AAAA",` +
		` "params": {"tools": [{"name": "extract"}]}}` + "\n"
	code, out := scriptRun{
		finding: chatFinding("anthropic"),
		stub:    stubShapeEcho,
		stubAs:  "anthropic.py",
		prompts: prompts,
		arg:     "prompts.jsonl",
		env:     "STUB_CANDIDATE=yes",
	}.run(t)
	if code != 0 {
		t.Errorf("exit = %d, want 0\n%s", code, out)
	}
	want := "shape: max_tokens=1024 tools=True parts=['image', 'text']"
	if !strings.Contains(out, want) {
		t.Errorf("output is missing %q\n%s", want, out)
	}
}

// A vision call site whose rows carry no image is not a comparison of
// that site, so the script refuses to answer rather than printing an
// agreement number about a call the code never makes.
func TestChatScriptRefusesAVisionSiteWithoutImages(t *testing.T) {
	cases := []struct {
		name     string
		prompts  string
		wantCode int
		wantOut  string
	}{
		{
			name:     "no row carries an image",
			prompts:  chatPrompts,
			wantCode: 2,
			wantOut:  "no row carries an image_url",
		},
		{
			name:     "the rows carry the site's image",
			prompts:  `{"prompt": "one", "image_url": "https://example.test/a.png"}` + "\n",
			wantCode: 0,
			wantOut:  "agreement is 100.0%, trips below 97%: held",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := chatFinding("anthropic")
			f.Archetype = scan.ArchetypeVision
			code, out := scriptRun{
				finding: f,
				stub:    stubAnthropic,
				stubAs:  "anthropic.py",
				prompts: tc.prompts,
				arg:     "prompts.jsonl",
				env:     "STUB_CANDIDATE=yes",
			}.run(t)
			if code != tc.wantCode {
				t.Errorf("exit = %d, want %d\n%s", code, tc.wantCode, out)
			}
			if !strings.Contains(out, tc.wantOut) {
				t.Errorf("output is missing %q\n%s", tc.wantOut, out)
			}
		})
	}
}

// The embedding script gates on the neighborhoods it measures, not on
// the chat metric.
func TestEmbeddingScriptExitsOnTripwire(t *testing.T) {
	cases := []struct {
		name     string
		flip     string
		wantCode int
		wantOut  string
	}{
		{
			name:     "neighborhoods hold",
			flip:     "0",
			wantCode: 0,
			wantOut:  "nearest_neighbor_agreement is 100.0%, trips below 90%: held",
		},
		{
			name:     "neighborhoods move",
			flip:     "1",
			wantCode: 1,
			wantOut:  "nearest_neighbor_agreement is 0.0%, trips below 90%: tripped, keep the current model",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := rules.Finding{
				RuleID: "pricey-embeddings", File: "rag/index.py", Line: 7,
				Model: "embed-big", CandidateModel: "embed-small",
				Tripwire: "If nearest neighbor agreement drops below 90%, stay put",
				TripwireCheck: rules.TripwireCheck{
					Metric: "nearest_neighbor_agreement", Compare: "below", Threshold: 90,
				},
			}
			code, out := scriptRun{
				finding: f,
				stub:    stubOpenAI,
				stubAs:  "openai.py",
				prompts: embeddingPrompts,
				arg:     "prompts.jsonl",
				env:     "STUB_FLIP=" + tc.flip,
			}.run(t)
			if code != tc.wantCode {
				t.Errorf("exit = %d, want %d\n%s", code, tc.wantCode, out)
			}
			if !strings.Contains(out, tc.wantOut) {
				t.Errorf("output is missing %q\n%s", tc.wantOut, out)
			}
		})
	}
}

// A tripwire is prose from a rule file. Whatever it holds, the script
// has to parse and print it back.
func TestTripwireProseSurvivesTheScript(t *testing.T) {
	f := chatFinding("anthropic")
	f.Tripwire = `If "agreement" drops below 97%, stay put \ here`
	body := generateOne(t, f)
	if !strings.Contains(body, `TRIPWIRE = "If \"agreement\" drops below 97%, stay put \\ here"`) {
		t.Errorf("script does not quote the tripwire for Python:\n%s", body)
	}
}
