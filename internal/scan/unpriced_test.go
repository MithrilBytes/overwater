package scan

import "testing"

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
