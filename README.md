# overwater

You would never use a fire hose to water your houseplants.

overwater scans a codebase for LLM call sites where the model in use is
bigger, pricier, or more wastefully configured than the task in front of it.
It nominates cheaper candidates with projected monthly savings, and its CI
guard fails builds that introduce new overwatering without punishing findings
that already exist.

The scanner reads code and never transmits it. It makes no model API calls,
and the only network request it can ever make is fetching the public model
catalog. A catalog snapshot ships inside the binary, so it also works fully
offline.

## Install

```bash
go install github.com/MithrilBytes/overwater/cmd/overwater@latest
```

## Usage

```bash
overwater scan path/to/repo
```

The scan prints one verdict block per finding, or the null verdict when
nothing clears the bar. `--json` emits the same findings for machines,
`--models-md` writes MODELS.md into the scanned repo, and `--volume`
overrides the default estimate of 10,000 calls per month per call site.
Scanning always exits 0 unless the run itself fails; the failure policy
for CI arrives with the baseline ratchet.

## Example

`fixtures/node-cron-summarizer` is a nightly digest job: a cron trigger
drives a frontier model over the realtime endpoint, and a legacy file
nobody deleted still points at a retired model.

```js
const response = await client.chat.completions.create({
  model: "gpt-5.1",
  max_tokens: 800,
  messages: [
    { role: "system", content: DIGEST_PROMPT },
    { role: "user", content: articles.join("\n\n") },
  ],
});

cron.schedule("0 6 * * *", runDigest);
```

`overwater scan fixtures/node-cron-summarizer` reports:

```
Prices from catalog 2026-08-05. Costs are estimates at 10,000 calls per
month per call site; override with --volume.

Call site: legacy/summarize-v1.js:7 (summarization; high confidence)
Current:   text-davinci-003 at ~$151/mo at estimated volume
Candidate: gpt-5-mini, current replacement in the same tier, ~$6/mo
Tripwire:  None; there is no configuration in which a retired model id keeps working
Flag:      Unavailable after 2024-01-04; a correctness bug, not just a cost one

Call site: src/summarize.js:12 (summarization: cron scheduled; medium confidence)
Current:   gpt-5.1 at ~$48/mo at estimated volume
Candidate: gpt-5.1 through the batch endpoint at half price, ~$24/mo
Tripwire:  If results are needed in under an hour, stay put
Flag:      None
```

When a repo is already right-sized (bounded outputs, cached prompts,
small models on small tasks), the whole verdict is:

```
Prices from catalog 2026-08-05.

Keep the models you have.
```

Both outputs come from running the scanner against this repo's own
fixtures, and the golden harness keeps them honest.

## How it judges a call site

Four detection layers feed a rules engine:

1. Manifests: which LLM SDKs the repo declares.
2. Model strings: every catalog id and alias, scanned across source, env
   files, and config. Unknown model-looking strings are reported at low
   confidence instead of ignored.
3. Call-site shape: temperature, max_tokens, schemas, tools, streaming,
   system prompt size, cache_control, read from a window around each hit.
   When the shape is unreadable, the scanner says so instead of guessing.
4. Archetype: extraction, classification, summarization, chat, agentic
   loop, or embedding.

Every rule, threshold, and price lives in data files (`rules/*.yaml` and
`catalog/`), not code. Every finding renders five fields:

```
Call site: src/classify.ts:57 (classification: temp 0, JSON schema; high confidence)
Current:   claude-opus-5 at ~$135/mo at estimated volume
Candidate: claude-haiku-4-5, same capability tier for this task class, ~$27/mo
Tripwire:  If eval agreement drops below 97%, stay put
Flag:      No prompt caching on a 1,191-token repeated system prompt
```

Findings are nominations, never directives; only an eval can prove a cheaper
model safe for your task. When nothing clears the confidence bar, the verdict
is exactly: "Keep the models you have."

## Status

Working today:

- The model catalog: 20 seed entries across five providers, one YAML file
  per model, validated and emitted as a single versioned `catalog.json`
  that is embedded in the binary. `overwater catalog build` and
  `overwater catalog show` work now.
- Five fixture repos under `fixtures/` and their golden verdicts under
  `goldens/`. The goldens are the spec.
- All four detection layers and the rules engine, exercised end to end by
  the test suite against every fixture.
- `overwater scan` with all three renderers (terminal, MODELS.md, and
  `--json`) driven by one shared findings object. The golden harness
  proves the MODELS.md renderer reproduces each fixture's golden byte
  for byte.

### Milestones to v1.0

Steps 1 through 6 of the build order landed 2026-08-05: the scaffold, the
catalog, the fixtures and goldens, all four detection layers, the rules
engine, and the renderers with their golden harness. What remains is
deliberate and ordered, and each milestone has an acceptance bar it must
clear before it counts.

**The guard (step 7).** The scan learns a failure policy: `--baseline
.overwater.json` compares findings by stable fingerprint (rule id plus
normalized path plus a call-site hash, so line drift does not break
matching), `--update-baseline` rewrites the file and prunes fixed
findings, and `--fail-on new|any|none` defaults to `new`. Done when tests
prove three things: a new finding fails the build, a baselined finding
passes, and a fixed finding is pruned on update.

**The Action (step 8).** A composite `action.yml` at the repo root:
downloads a pinned release binary, verifies its checksum, runs the scan,
writes the findings table to the step summary, and exits with the
scanner's code. Zero secrets, read permission only. Done when the dogfood
workflow in this repo runs it against ts-chat-firehose expecting exit 1
and against clean-app expecting exit 0, and both are green.

**The living catalog (step 9).** Optional HTTPS fetch of the published
catalog with a local cache, a "prices as of" staleness warning instead of
a failure, and `--offline` forbidding all network activity. Done when a
network-denying transport test proves the scanner makes zero requests
under `--offline` and only the single catalog request otherwise.

**Generated evals (step 10).** `overwater eval` writes one runnable A/B
script per finding, with both model ids filled in. The user supplies a
JSONL of real prompts and their own keys, runs it outside the scanner,
and reads an agreement percentage against the tripwire. Done when a
generated script exists for every fixture finding and names exactly the
models its finding names.

**v1.0.** All of the above, plus release binaries for macOS, Linux, and
Windows and a CI setup snippet in this README. Tagged only when the
dogfood workflow is green and the full suite passes with networking
disabled.

### Scope for v1

In scope: the advisor scan, the CI guard with a baseline ratchet so legacy
findings never fail a build, the public versioned catalog, and generated A/B
eval scripts that the user runs with their own keys outside the scanner.

Out of scope, deliberately: PR comment mode, a scheduled upstream price-diff
action, a built-in eval runner, tree-sitter or full AST parsing, editor
plugins, a hosted service of any kind, Homebrew packaging, and telemetry of
any kind.

## Exit codes

`0` clean, or every finding is baselined. `1` findings that violate the
failure policy. `2` operational error. They are never conflated, so CI can
script against them.

## License

MIT. See [LICENSE](LICENSE).
