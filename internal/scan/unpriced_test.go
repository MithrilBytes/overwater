package scan

import (
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"
)

// appium-mcp: the billing call is a fetch to a template literal URL, and
// the model is a runtime variable. The audit that found this measured
// files=0 sites=0 for the file that spends the money.
func TestEndpointWithoutAModel(t *testing.T) {
	dir := writeTree(t, map[string]string{
		"vision.ts": "const r = await fetch(`${cfg.apiBaseUrl}/chat/completions`, {\n" +
			"  body: JSON.stringify({ model: cfg.model }),\n});\n",
		"anthropic.py": "requests.post(f\"{base}/v1/messages\", json=payload)\n",
		"gemini.go":    "url := base + \"/models/\" + name + \":generateContent\"\n",
		"ollama.js":    "await fetch(`${host}/api/generate`, { method: 'POST' });\n",
	})
	report, err := Analyze(dir, mustCatalog(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Unpriced) != 4 {
		t.Fatalf("unpriced = %d, want 4: %+v", len(report.Unpriced), report.Unpriced)
	}
	for _, u := range report.Unpriced {
		if u.Kind != "endpoint" {
			t.Errorf("%s:%d kind = %q, want endpoint", u.File, u.Line, u.Kind)
		}
	}
}

// sparkwing: a benchmark harness that shells out to agent CLIs. Real
// token spend, no SDK, no endpoint, and the model is either an alias the
// catalog has never carried or absent entirely.
func TestAgentCLIWithoutAModel(t *testing.T) {
	dir := writeTree(t, map[string]string{
		"bin/trial.sh": "#!/usr/bin/env bash\n" +
			"result=\"$(claude --print --output-format json <<<\"$spec\")\"\n" +
			"run_one claude-opus --agent claude --model opus\n" +
			"codex exec --json \"$prompt\"\n",
	})
	report, err := Analyze(dir, mustCatalog(t))
	if err != nil {
		t.Fatal(err)
	}
	var cli []UnpricedCall
	for _, u := range report.Unpriced {
		if u.Kind == "agent-cli" {
			cli = append(cli, u)
		}
	}
	// Lines 2 and 4 have no model text at all. Line 3 is not among them,
	// and should not be: "claude-opus" is model looking, so that line
	// already produced a site and reaches the reader as an unrecognised
	// model instead. One line, one report.
	if len(cli) != 2 {
		t.Fatalf("agent-cli calls = %+v, want the two lines with no model text", cli)
	}
	for _, u := range cli {
		if u.Line == 3 {
			t.Errorf("line 3 was reported twice: it has a site for claude-opus")
		}
	}
	found := false
	for _, s := range report.Sites {
		if s.Line == 3 && !s.Known {
			found = true
		}
	}
	if !found {
		t.Errorf("line 3 lost its claude-opus site: %+v", report.Sites)
	}
}

// A line that already carries a model reference has a site, and a site
// is the better answer. Reporting both would double count the same call.
func TestPricedCallIsNotAlsoUnpriced(t *testing.T) {
	dir := writeTree(t, map[string]string{
		"a.ts": "await fetch(`${base}/chat/completions`, { body: JSON.stringify({ model: \"gpt-4o\" }) });\n",
	})
	report, err := Analyze(dir, mustCatalog(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Sites) != 1 {
		t.Fatalf("sites = %+v, want the gpt-4o site", report.Sites)
	}
	if len(report.Unpriced) != 0 {
		t.Errorf("unpriced = %+v, want none; the line already has a site", report.Unpriced)
	}
}

// Seven repositories that call no LLM produced no unpriced calls between
// them. These are the shapes that would have broken that: ordinary
// English, ordinary CLI flags, and an endpoint path that is not one.
func TestUnpricedCallsAreNotNoise(t *testing.T) {
	dir := writeTree(t, map[string]string{
		"a.go":       "// completions of the task\nvar chat = \"/chat/history\"\n",
		"b.ts":       "const url = `${base}/api/generated-report`;\n",
		"deploy.sh":  "#!/bin/bash\nkubectl exec -it pod -- bash\ndocker exec app ls\n",
		"build.sh":   "#!/bin/bash\ngit -p log\nmake --print-directory\n",
		"c.py":       "response = requests.post(f\"{base}/v1/users\", json=payload)\n",
		"README.txt": "Run claude --print to try it.\n",
	})
	report, err := Analyze(dir, mustCatalog(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Unpriced) != 0 {
		t.Errorf("unpriced = %+v, want none", report.Unpriced)
	}
}

// Documentation and tests are excluded from sites, and an unpriced call
// is a call: the same exclusion has to apply or a README showing how to
// run an agent CLI becomes spend.
func TestUnpricedCallsRespectFileRules(t *testing.T) {
	dir := writeTree(t, map[string]string{
		"docs/guide.md":     "Run `claude --print` against the spec.\n",
		"bin/trial_test.sh": "claude --print --model opus\n",
		"tests/smoke.sh":    "claude --print --model opus\n",
	})
	report, err := Analyze(dir, mustCatalog(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Unpriced) != 0 {
		t.Errorf("unpriced = %+v, want none from docs or tests", report.Unpriced)
	}
}

// The evidence is printed on stderr, which CI puts in the job log and
// in a pull request comment. The lines that match these patterns are
// the lines that carry credentials, so the evidence is what matched and
// nothing around it: the bearer token, the --api-key argument and the
// gateway host below all used to be reprinted verbatim.
func TestUnpricedEvidenceCarriesNoCredentials(t *testing.T) {
	dir := writeTree(t, map[string]string{
		"run.sh": "#!/bin/bash\n" +
			"curl -H \"Authorization: Bearer sk-curl-FAKE-5555\" https://api.openai.com/v1/chat/completions\n" +
			// The key sits before the flag that triggers the match, where
			// the pattern's lazy quantifier reaches straight over it.
			"claude --api-key sk-ant-FAKE-4444 --print < prompt.txt\n",
		"config.yaml": "endpoint: https://llm-gw.internal.acme.corp/v1/chat/completions?api-key=hunter2\n",
	})
	report, err := Analyze(dir, mustCatalog(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Unpriced) != 3 {
		t.Fatalf("unpriced = %+v, want three calls", report.Unpriced)
	}
	for _, u := range report.Unpriced {
		for _, secret := range []string{"sk-curl-FAKE-5555", "sk-ant-FAKE-4444", "hunter2",
			"Authorization", "--api-key", "acme.corp"} {
			if strings.Contains(u.Evidence, secret) {
				t.Errorf("%s:%d evidence = %q, want no %q", u.File, u.Line, u.Evidence, secret)
			}
		}
	}
	want := map[string]string{
		"config.yaml:1": "/v1/chat/completions",
		"run.sh:2":      "/v1/chat/completions",
		"run.sh:3":      "claude --print",
	}
	for _, u := range report.Unpriced {
		key := u.File + ":" + strconv.Itoa(u.Line)
		if got := want[key]; got != u.Evidence {
			t.Errorf("%s evidence = %q, want %q", key, u.Evidence, got)
		}
	}
}

// The evidence line is printed on stderr, so it has to be valid UTF-8.
// It was not: truncation counted bytes, so a repository with an Arabic
// string inside a fetch call emitted a split character. That is not
// cosmetic, it crashed a harness reading the output mid sweep.
func TestUnpricedEvidenceIsValidUTF8(t *testing.T) {
	long := strings.Repeat("مفتاح Bearer لخادم ", 12)
	dir := writeTree(t, map[string]string{
		"a.ts": "await fetch(`${base}/chat/completions`, { headers: { hint: \"" + long + "\" } });\n",
	})
	report, err := Analyze(dir, mustCatalog(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Unpriced) != 1 {
		t.Fatalf("unpriced = %+v, want one endpoint call", report.Unpriced)
	}
	ev := report.Unpriced[0].Evidence
	if !utf8.ValidString(ev) {
		t.Errorf("evidence is not valid utf-8: %q", ev)
	}
	if n := utf8.RuneCountInString(strings.TrimSuffix(ev, "...")); n > unpricedEvidenceMax {
		t.Errorf("evidence is %d runes, want at most %d", n, unpricedEvidenceMax)
	}
}
