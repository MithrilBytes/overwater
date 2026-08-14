package scan

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"
)

// wrapperLib: one helper holds the SDK call, everything else calls the
// helper.
const wrapperLib = `import Anthropic from "@anthropic-ai/sdk";

const client = new Anthropic();

export async function complete(prompt, model = "claude-opus-5") {
  return client.messages.create({
    model,
    max_tokens: 1024,
    messages: [{ role: "user", content: prompt }],
  });
}
`

func fanInSite(t *testing.T, r *Report, file string) Site {
	t.Helper()
	for _, s := range r.Sites {
		if s.File == file {
			return s
		}
	}
	t.Fatalf("no site in %s: %+v", file, r.Sites)
	return Site{}
}

// One call site in llm.js, three calls to it across two other files.
func TestFanInAcrossFiles(t *testing.T) {
	r := analyzeTemp(t, map[string]string{
		"llm.js": wrapperLib,
		"a.js": `const { complete } = require("./llm");

async function summarizeArticle(text) {
  return complete(text);
}
`,
		"b.js": `const { complete } = require("./llm");

async function draftReply(text) {
  const first = await complete(text);
  return complete(first);
}
`,
	})
	site := fanInSite(t, r, "llm.js")
	if site.FanInFunc != "complete" {
		t.Errorf("FanInFunc = %q, want complete", site.FanInFunc)
	}
	if site.FanInStatus != FanInExact || site.FanIn != 3 {
		t.Errorf("fan in = %d (%s), want 3 exact", site.FanIn, site.FanInStatus)
	}
}

// The count tracks the caller count; it does not saturate at a cap.
func TestFanInScalesWithCallers(t *testing.T) {
	files := map[string]string{"llm.js": wrapperLib}
	for i := 0; i < 40; i++ {
		files[fmt.Sprintf("use%02d.js", i)] = fmt.Sprintf(
			"const { complete } = require(\"./llm\");\n\nasync function use%02d(t) {\n  return complete(t);\n}\n", i)
	}
	site := fanInSite(t, analyzeTemp(t, files), "llm.js")
	if site.FanIn != 40 || site.FanInStatus != FanInExact {
		t.Errorf("fan in = %d (%s), want 40 exact", site.FanIn, site.FanInStatus)
	}
}

// The typed TypeScript spelling, called from module scope in files that
// hold no model string of their own.
func TestFanInTypedTSWrapper(t *testing.T) {
	files := map[string]string{
		"llm.ts": `import Anthropic from "@anthropic-ai/sdk";

const client = new Anthropic();

export async function complete(prompt: string, model = "claude-opus-5") {
  return client.messages.create({
    model,
    max_tokens: 1024,
    messages: [{ role: "user", content: prompt }],
  });
}
`,
	}
	for i := 0; i < 4; i++ {
		files[fmt.Sprintf("page%d.ts", i)] = fmt.Sprintf(
			"import { complete } from \"./llm\";\n\nexport const summary%d = await complete(articleText);\n", i)
	}
	r := analyzeTemp(t, files)
	if len(r.Sites) != 1 {
		t.Fatalf("sites = %d, want 1: only the wrapper names a model", len(r.Sites))
	}
	site := r.Sites[0]
	if site.FanIn != 4 || site.FanInStatus != FanInExact {
		t.Errorf("fan in = %d (%s), want 4 exact", site.FanIn, site.FanInStatus)
	}
	want := []CallerModel{{Ref: "claude-opus-5", ModelID: "claude-opus-5", Known: true, Count: 4}}
	if !reflect.DeepEqual(site.CallerModels, want) {
		t.Errorf("caller models = %+v, want %+v", site.CallerModels, want)
	}
}

// Two definitions of one name: the count stays at the floor rather than
// crediting one wrapper with the other's callers.
func TestFanInAmbiguousName(t *testing.T) {
	r := analyzeTemp(t, map[string]string{
		"a/llm.js": wrapperLib,
		"b/llm.js": strings.Replace(wrapperLib, "claude-opus-5", "claude-sonnet-5", 1),
		"use.js": `async function run(t) {
  return complete(t);
}
`,
	})
	for _, file := range []string{"a/llm.js", "b/llm.js"} {
		site := fanInSite(t, r, file)
		if site.FanInStatus != FanInAmbiguous || site.FanIn != 1 {
			t.Errorf("%s fan in = %d (%s), want 1 ambiguous", file, site.FanIn, site.FanInStatus)
		}
		if len(site.CallerModels) != 0 {
			t.Errorf("%s caller models = %+v, want none for an ambiguous name", file, site.CallerModels)
		}
	}
}

// A helper nobody in the repo calls is an entry point or an export:
// unresolved, and 1 is a floor rather than a count.
func TestFanInUnresolved(t *testing.T) {
	site := fanInSite(t, analyzeTemp(t, map[string]string{"llm.js": wrapperLib}), "llm.js")
	if site.FanInStatus != FanInUnresolved || site.FanIn != 1 {
		t.Errorf("fan in = %d (%s), want 1 unresolved", site.FanIn, site.FanInStatus)
	}
}

// A model string at file scope runs where it sits.
func TestFanInFileScopeSite(t *testing.T) {
	r := analyzeTemp(t, map[string]string{"models.ts": `export const FALLBACKS = ["gpt-4o-mini"];

export async function route(text: string) {
  return client.chat.completions.create({ model: "gpt-5-mini", max_tokens: 100, messages: text });
}
`})
	var scoped, inFunc *Site
	for i := range r.Sites {
		switch r.Sites[i].Ref {
		case "gpt-4o-mini":
			scoped = &r.Sites[i]
		case "gpt-5-mini":
			inFunc = &r.Sites[i]
		}
	}
	if scoped == nil || inFunc == nil {
		t.Fatalf("sites = %+v", r.Sites)
	}
	if scoped.FanInStatus != FanInDirect || scoped.FanIn != 1 || scoped.FanInFunc != "" {
		t.Errorf("file scope site = %d (%s) in %q, want 1 direct with no function",
			scoped.FanIn, scoped.FanInStatus, scoped.FanInFunc)
	}
	if inFunc.FanInFunc != "route" {
		t.Errorf("in function site = %q, want route", inFunc.FanInFunc)
	}
}

// A wrapper whose model is a parameter, called with one model in most
// places and another in the rest.
func TestCallerModelsFromLiterals(t *testing.T) {
	files := map[string]string{"llm.js": wrapperLib}
	for i := 0; i < 3; i++ {
		files[fmt.Sprintf("cheap%d.js", i)] = fmt.Sprintf(
			"async function cheap%d(t) {\n  return complete(t, \"claude-haiku-4-5\");\n}\n", i)
	}
	for i := 0; i < 5; i++ {
		files[fmt.Sprintf("plain%d.js", i)] = fmt.Sprintf(
			"async function plain%d(t) {\n  return complete(t);\n}\n", i)
	}
	site := fanInSite(t, analyzeTemp(t, files), "llm.js")
	if site.FanIn != 8 {
		t.Fatalf("fan in = %d, want 8", site.FanIn)
	}
	want := []CallerModel{
		{Ref: "claude-opus-5", ModelID: "claude-opus-5", Known: true, Count: 5},
		{Ref: "claude-haiku-4-5", ModelID: "claude-haiku-4-5", Known: true, Count: 3},
	}
	if !reflect.DeepEqual(site.CallerModels, want) {
		t.Errorf("caller models = %+v, want %+v", site.CallerModels, want)
	}
}

// Callers that pass nothing get the default, and the default is the
// literal the site was found at.
func TestCallerModelsDefaultParameter(t *testing.T) {
	site := fanInSite(t, analyzeTemp(t, map[string]string{
		"llm.js": wrapperLib,
		"use.js": `async function one(t) {
  return complete(t);
}

async function two(t) {
  return complete(t);
}
`,
	}), "llm.js")
	want := []CallerModel{{Ref: "claude-opus-5", ModelID: "claude-opus-5", Known: true, Count: 2}}
	if !reflect.DeepEqual(site.CallerModels, want) {
		t.Errorf("caller models = %+v, want %+v", site.CallerModels, want)
	}
}

// Python keyword arguments name the parameter wherever they sit, and a
// caller may pass a named constant rather than a literal.
func TestCallerModelsPythonKeyword(t *testing.T) {
	site := fanInSite(t, analyzeTemp(t, map[string]string{
		"llm.py": `import anthropic

client = anthropic.Anthropic()


def complete(prompt, model="claude-opus-5"):
    return client.messages.create(
        model=model,
        max_tokens=1024,
        messages=[{"role": "user", "content": prompt}],
    )
`,
		"jobs.py": `from llm import complete

CHEAP = "claude-haiku-4-5"


def nightly(text):
    return complete(text, model=CHEAP)


def interactive(text):
    return complete(text)
`,
	}), "llm.py")
	if site.FanIn != 2 || site.FanInStatus != FanInExact {
		t.Fatalf("fan in = %d (%s), want 2 exact", site.FanIn, site.FanInStatus)
	}
	want := []CallerModel{
		{Ref: "claude-haiku-4-5", ModelID: "claude-haiku-4-5", Known: true, Count: 1},
		{Ref: "claude-opus-5", ModelID: "claude-opus-5", Known: true, Count: 1},
	}
	if !reflect.DeepEqual(site.CallerModels, want) {
		t.Errorf("caller models = %+v, want %+v", site.CallerModels, want)
	}
}

// The default written as a constant: the site sits at file scope, but
// the traffic is at the wrapper's callers.
func TestCallerModelsDefaultConstant(t *testing.T) {
	site := fanInSite(t, analyzeTemp(t, map[string]string{
		"llm.ts": `const DEFAULT_MODEL = "claude-opus-5";

export async function complete(prompt: string, model: string = DEFAULT_MODEL) {
  return client.messages.create({
    model,
    max_tokens: 1024,
    messages: [{ role: "user", content: prompt }],
  });
}
`,
		"use.ts": `import { complete } from "./llm";

export async function summarize(t: string) {
  return complete(t);
}

export async function label(t: string) {
  return complete(t, "claude-haiku-4-5");
}
`,
	}), "llm.ts")
	if site.FanInFunc != "complete" || site.FanIn != 2 || site.FanInStatus != FanInExact {
		t.Fatalf("fan in = %d (%s) in %q, want 2 exact in complete",
			site.FanIn, site.FanInStatus, site.FanInFunc)
	}
	want := []CallerModel{
		{Ref: "claude-haiku-4-5", ModelID: "claude-haiku-4-5", Known: true, Count: 1},
		{Ref: "claude-opus-5", ModelID: "claude-opus-5", Known: true, Count: 1},
	}
	if !reflect.DeepEqual(site.CallerModels, want) {
		t.Errorf("caller models = %+v, want %+v", site.CallerModels, want)
	}
}

// Two wrappers sharing one default constant cannot both claim the site,
// so neither does.
func TestCallerModelsSharedConstant(t *testing.T) {
	site := fanInSite(t, analyzeTemp(t, map[string]string{
		"llm.ts": `const DEFAULT_MODEL = "claude-opus-5";

export async function ask(prompt: string, model: string = DEFAULT_MODEL) {
  return client.messages.create({ model, max_tokens: 100, messages: prompt });
}

export async function tell(prompt: string, model: string = DEFAULT_MODEL) {
  return client.messages.create({ model, max_tokens: 100, messages: prompt });
}
`,
		"use.ts": `export async function run(t: string) {
  return ask(t);
}
`,
	}), "llm.ts")
	if site.FanInStatus != FanInDirect || site.FanInFunc != "" {
		t.Errorf("fan in = %d (%s) in %q, want direct: two wrappers share the constant",
			site.FanIn, site.FanInStatus, site.FanInFunc)
	}
}

// A destructured options object is the common TypeScript spelling, and
// callers fill it by name.
func TestCallerModelsObjectParameter(t *testing.T) {
	site := fanInSite(t, analyzeTemp(t, map[string]string{
		"llm.ts": `export async function ask({ prompt, model = "claude-opus-5" }) {
  return client.messages.create({
    model,
    max_tokens: 512,
    messages: [{ role: "user", content: prompt }],
  });
}
`,
		"use.ts": `export async function cheap(t: string) {
  return ask({ prompt: t, model: "claude-haiku-4-5" });
}

export async function normal(t: string) {
  return ask({ prompt: t });
}
`,
	}), "llm.ts")
	want := []CallerModel{
		{Ref: "claude-haiku-4-5", ModelID: "claude-haiku-4-5", Known: true, Count: 1},
		{Ref: "claude-opus-5", ModelID: "claude-opus-5", Known: true, Count: 1},
	}
	if !reflect.DeepEqual(site.CallerModels, want) {
		t.Errorf("caller models = %+v, want %+v", site.CallerModels, want)
	}
}

// A caller that forwards its own parameter is followed one hop back to
// whoever decided the value.
func TestCallerModelsForwarded(t *testing.T) {
	site := fanInSite(t, analyzeTemp(t, map[string]string{
		"llm.js": wrapperLib,
		"mid.js": `async function ask(text, model) {
  return complete(text, model);
}

async function cheap(text) {
  return ask(text, "claude-haiku-4-5");
}
`,
	}), "llm.js")
	if len(site.CallerModels) != 1 || site.CallerModels[0].Ref != "claude-haiku-4-5" {
		t.Errorf("caller models = %+v, want claude-haiku-4-5 through the forwarding wrapper",
			site.CallerModels)
	}
}

// Self and mutual recursion must terminate; the scan returning at all
// is half the assertion.
func TestFanInRecursionTerminates(t *testing.T) {
	r := analyzeTemp(t, map[string]string{
		"loop.js": `async function retryComplete(text, model = "claude-opus-5") {
  const out = await client.messages.create({ model, max_tokens: 100, messages: text });
  return out || retryComplete(text, model);
}

async function ping(text, model) {
  return pong(text, model);
}

async function pong(text, model) {
  return ping(text, model);
}

async function start(text) {
  return ping(text, "claude-haiku-4-5");
}
`,
	})
	site := fanInSite(t, r, "loop.js")
	if site.FanInFunc != "retryComplete" {
		t.Fatalf("FanInFunc = %q, want retryComplete", site.FanInFunc)
	}
	if site.FanIn != 1 || site.FanInStatus != FanInExact {
		t.Errorf("fan in = %d (%s), want 1 exact from the recursive call", site.FanIn, site.FanInStatus)
	}
}

// A model shaped string inside a prompt is prose, not a wrapper call.
func TestFanInIgnoresCallsInStrings(t *testing.T) {
	site := fanInSite(t, analyzeTemp(t, map[string]string{
		"llm.js": wrapperLib,
		"docs.js": `const HELP = "Call complete(text) to reach the model, then complete(text) again.";

module.exports = { HELP };
`,
	}), "llm.js")
	if site.FanInStatus != FanInUnresolved {
		t.Errorf("fan in = %d (%s), want unresolved: the calls are inside a string",
			site.FanIn, site.FanInStatus)
	}
}

// An SDK method that shares a name with a repo function is not a call
// to it. Only bare and self calls count.
func TestFanInSkipsSDKMethodCalls(t *testing.T) {
	site := fanInSite(t, analyzeTemp(t, map[string]string{
		"llm.js": `function create(prompt, model = "claude-opus-5") {
  return prompt + model;
}
`,
		"use.js": `async function run(text) {
  return client.messages.create({ model: "claude-haiku-4-5", max_tokens: 10, messages: text });
}
`,
	}), "llm.js")
	if site.FanInStatus != FanInUnresolved {
		t.Errorf("fan in = %d (%s), want unresolved: client.messages.create is not the repo function",
			site.FanIn, site.FanInStatus)
	}
}

// A Python method reached through self counts, since that is how a
// class shaped wrapper is called.
func TestFanInCountsSelfCalls(t *testing.T) {
	site := fanInSite(t, analyzeTemp(t, map[string]string{"svc.py": `class Router:
    def complete(self, prompt, model="claude-opus-5"):
        return client.messages.create(
            model=model,
            max_tokens=256,
            messages=[{"role": "user", "content": prompt}],
        )

    def summarize(self, text):
        return self.complete(text)

    def label(self, text):
        return self.complete(text, model="claude-haiku-4-5")
`}), "svc.py")
	if site.FanIn != 2 || site.FanInStatus != FanInExact {
		t.Errorf("fan in = %d (%s), want 2 exact from the self calls", site.FanIn, site.FanInStatus)
	}
}

// Fan in and the caller models must be identical across runs; the
// goldens depend on it.
func TestFanInIsDeterministic(t *testing.T) {
	files := map[string]string{"llm.js": wrapperLib}
	for i := 0; i < 6; i++ {
		model := "claude-haiku-4-5"
		if i%2 == 0 {
			model = "claude-sonnet-5"
		}
		files[fmt.Sprintf("use%d.js", i)] = fmt.Sprintf(
			"async function use%d(t) {\n  return complete(t, \"%s\");\n}\n", i, model)
	}
	dir := writeTree(t, files)
	cat := mustCatalog(t)
	var first *Report
	for i := 0; i < 10; i++ {
		r, err := Analyze(dir, cat)
		if err != nil {
			t.Fatal(err)
		}
		if first == nil {
			first = r
			continue
		}
		if !reflect.DeepEqual(first, r) {
			t.Fatalf("run %d differs from run 0:\nfirst: %+v\n  got: %+v", i, first.Sites, r.Sites)
		}
	}
	site := fanInSite(t, first, "llm.js")
	if len(site.CallerModels) != 2 || site.CallerModels[0].Ref != "claude-haiku-4-5" {
		t.Errorf("caller models = %+v, want haiku first on a tie broken by name", site.CallerModels)
	}
}

// The index reads two definition patterns by regex and the third by
// byte scan. Both paths together must see every definition the extent
// walker does, or adding a pattern there silently stops counting a
// language here.
func TestIndexSeesEveryDefPattern(t *testing.T) {
	src := `func describe(site *Site, tier string) {
	return nil
}

function classifyTicket(body) {}

const draftReply = async (ticket) => {};

def extract_invoice(text):
    return text
`
	a := newAnalyzer([]file{{path: "mixed.go", data: src}})
	got := map[string]bool{}
	for _, d := range a.fileDefs("mixed.go") {
		got[d.name] = true
	}
	for _, re := range reFuncDefs {
		for _, m := range re.FindAllStringSubmatch(src, -1) {
			if !funcNameStopwords[m[1]] && !got[m[1]] {
				t.Errorf("%q found by the extent walker but missing from the index", m[1])
			}
		}
	}
	for _, want := range []string{"describe", "classifyTicket", "draftReply", "extract_invoice"} {
		if !got[want] {
			t.Errorf("index missed %q", want)
		}
	}
}

// The index is a second pass over every file, so it must stay linear in
// file size. A minified bundle is one line holding thousands of calls,
// where a backward scan per call turns quadratic. The bound is generous
// by three orders of magnitude.
func TestIndexBoundedInMinified(t *testing.T) {
	var b strings.Builder
	b.WriteString("function boot(){")
	for i := 0; i < 6000; i++ {
		fmt.Fprintf(&b, `send({model:"gpt-4o-mini",max_tokens:%d});`, i)
	}
	b.WriteString("}")
	a := newAnalyzer([]file{{path: "bundle.js", data: b.String()}})
	start := time.Now()
	idx := a.index()
	if elapsed := time.Since(start); elapsed > 10*time.Second {
		t.Fatalf("indexing a %d byte minified file took %v", b.Len(), elapsed)
	}
	if len(idx.byName["boot"]) != 1 {
		t.Errorf("boot definitions = %d, want 1", len(idx.byName["boot"]))
	}
	if d := idx.byName["boot"][0]; d.end != b.Len() {
		t.Errorf("boot ends at %d, want the end of the file at %d", d.end, b.Len())
	}
}

// The five shipped fixtures hold no wrapper of this shape, so fan in
// must not move what they report. Every fixture site is direct or a
// function with at most one visible caller.
func TestFixturesHaveNoWrappers(t *testing.T) {
	for _, fixture := range []string{
		"clean-app", "node-cron-summarizer", "py-extraction",
		"rag-frontier-embeddings", "ts-chat-firehose",
	} {
		r, err := Analyze("../../fixtures/"+fixture, mustCatalog(t))
		if err != nil {
			t.Fatal(err)
		}
		for _, s := range r.Sites {
			if s.FanIn != 1 {
				t.Errorf("%s: %s:%d fan in = %d, want 1", fixture, s.File, s.Line, s.FanIn)
			}
			if len(s.CallerModels) != 0 {
				t.Errorf("%s: %s:%d caller models = %+v, want none",
					fixture, s.File, s.Line, s.CallerModels)
			}
		}
	}
}
