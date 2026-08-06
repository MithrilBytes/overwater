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

Or take a release binary: every release ships static builds for macOS,
Linux, and Windows, plus a SHA256SUMS file to verify them against.

## Usage

```bash
overwater scan path/to/repo
```

The scan prints one verdict block per finding, or the null verdict when
nothing clears the bar. `--json` emits the same findings for machines,
`--models-md` writes MODELS.md into the scanned repo, and `--volume`
overrides the default estimate of 10,000 calls per month per call site.
By default the scan is advice: it exits 0 whenever the run itself
succeeds. In CI, record a baseline once, commit it, and let the ratchet
fail only what is new:

```bash
overwater scan --baseline .overwater.json --update-baseline
overwater scan --baseline .overwater.json
```

`--fail-on any` fails on any finding, `none` never fails, and the
default `new` fails only on findings missing from the baseline.
Findings are matched by a fingerprint of the call site itself, not its
line number, so code moving around a file does not churn the baseline,
and fixed findings fall out the next time you record it.

When the classifier reads a call site wrong, pin it with a comment on or
just above the call:

```ts
// overwater:archetype=summarization
const result = await generateText({ ... });
```

In CI, the published action wraps all of this: pinned release binary,
checksum verified, findings in the job summary, no secrets, read
permission only.

```yaml
- uses: actions/checkout@v4
- uses: MithrilBytes/overwater@v1.5.1
  with:
    baseline: .overwater.json
```

Add `pr-comment: true` to get the verdict as a sticky comment on pull
requests; that needs `pull-requests: write` in the calling workflow.

Two more subcommands round out the tool. `overwater eval` writes one
runnable A/B script per finding that nominates a different model; you
bring a JSONL of real prompts and your own keys, and the script reports
agreement for you to judge against the tripwire. `overwater catalog
refresh` fetches the published catalog into a local cache, `scan
--refresh` does the same before scanning, and `--offline` forbids all
network activity. The default is still zero network: the embedded
snapshot and the cache carry the prices.

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
   system prompt size, cache_control. TypeScript and JavaScript call
   sites are parsed structurally: callee chain plus the properties of
   the object that names the model, one level into config wrappers.
   Other languages read the call's bracket-balanced extent over comment-
   and prose-masked source, so a neighboring call cannot bleed its
   parameters in and a prompt that merely mentions temperature cannot
   fake one. System prompts and schemas referenced by name are resolved
   in the same file or one import hop away. When the shape is
   unreadable, the scanner says so instead of guessing.
4. Archetype: extraction, classification, summarization, chat, agentic
   loop, or embedding, scored from weighted evidence: the enclosing
   function name counts most, then the resolved prompt text and schema
   semantics (an enum-only schema reads as classification, a many-field
   one as extraction), then nearby identifiers. The winner carries a
   graded confidence; a narrow win is reported as low confidence and
   demotes any finding that leans on it.

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

- The model catalog: 75 entries across nine providers, one YAML file per
  model, validated and emitted as a single versioned `catalog.json` that
  is embedded in the binary. Deprecated and retired ids stay in the
  catalog on purpose; they are exactly what the deprecated-model rule
  detects in legacy code. `overwater catalog build` and
  `overwater catalog show` work now.
- Five fixture repos under `fixtures/` and their golden verdicts under
  `goldens/`. The goldens are the spec.
- All four detection layers and the rules engine, exercised end to end by
  the test suite against every fixture.
- `overwater scan` with all three renderers (terminal, MODELS.md, and
  `--json`) driven by one shared findings object. The golden harness
  proves the MODELS.md renderer reproduces each fixture's golden byte
  for byte.
- The baseline ratchet: `--baseline`, `--update-baseline`, and
  `--fail-on` turn the scan into a CI guard that fails on new
  overwatering without punishing what a repo already had.
- Catalog refresh with a local cache and `--offline`. A transport test
  proves the scanner makes zero requests by default and exactly one
  when asked to refresh.
- `overwater eval`: one generated A/B script per finding, run by you
  with your own keys, never by the scanner.
- The GitHub Action, pinned to a release binary by checksum, dogfooded
  in this repo against the firehose and clean-app fixtures, with opt-in
  PR comments.
- Structural parsing for TypeScript call sites; a labeled corpus pins
  classifier accuracy (currently 13/14) with a floor in tests.
- A nightly price-watch that diffs the catalog against LiteLLM and
  opens a PR when prices move.

### v1.5

Current release line. On top of the ten v1 build steps:

- structural parsing for TypeScript call sites, pure Go (tree-sitter
  needs cgo and would break the static release binaries)
- nightly price-watch job: diff the catalog against LiteLLM, open a PR
  when prices move
- opt-in PR comments in the Action
- labeled corpus with a classifier accuracy floor enforced in tests

### Next

One branch per area, roughly ordered inside each. Nothing here is
promised; items get cut when a cheaper answer shows up.

- Parsers
  - Python
    - indentation aware call extents, kwargs, dict configs
    - notebooks: parse .ipynb cells as Python
    - resolve constants through module imports, not just one hop
    - keep the walk out of .venv and site-packages so runtime stays flat
    - Python 3 first; legacy Python 2 files keep the regex path
  - Go: struct literal params, MessageNewParams{Model: ...}
  - Java and Kotlin: builder chains, plus pom.xml and gradle manifests
    for layer 1
  - C#: object initializers, csproj manifests
  - Ruby: keyword args
  - PHP: named args, composer.json manifests
  - Rust: builder patterns, Cargo.toml manifests
  - Swift: Package.swift manifests
  - shell: model ids inside curl payloads
  - wrappers: a helper that forwards to the client should not hide the
    call site
  - config tracing: tie a MODEL env var or yaml key back to the call
    that reads it
  - Azure OpenAI: deployment names are user chosen, so detection needs
    the config trace, not the dictionary
  - tsconfig path aliases and monorepo workspaces for the import hop
- Classifier
  - corpus past a hundred cases, balanced across languages and
    providers
  - per archetype precision and recall, not one accuracy number
  - raise the floor as the corpus grows; never lower it
  - new archetypes: translation, reranking, moderation, transcription,
    code generation, vision extraction
  - confidence calibration: low should be wrong more often than high,
    measured
  - more pragmas: overwater:ignore for a site, overwater:volume for a
    known call rate
- Rules
  - reasoning effort overkill: high effort settings on lookup shaped
    tasks
  - retry amplification: aggressive retry config in front of frontier
    prices
  - fallback chains that name retired models
  - embedding dimension overkill where the API takes a dimensions
    parameter
  - image detail high on thumbnail sized inputs
  - realtime transcription models on batch audio jobs
  - budget rule: fail when projected monthly total crosses a number
    you set
  - temperature above zero on extraction shapes, flagged as
    correctness
  - duplicate call sites that could share one cached result
  - per repo config file: disable rules, tune thresholds, point at a
    volume file instead of the 10,000 per month default
- Catalog
  - cache read and write rates per entry, so caching findings price
    exactly instead of flagging
  - batch endpoint rates per provider instead of one multiplier
  - context windows and deprecation dates in the nightly price-watch
  - capability flags per entry: vision, tools, caching, structured
    output, audio
  - hosted open weight providers with their own pricing pages: groq,
    fireworks, together
  - international providers: qwen, moonshot, zhipu
  - bedrock and vertex id variants as aliases on existing entries
  - dated price snapshots kept in the repo, so drift has a history
- Guard
  - baseline aging: entries expire after a set time and start nagging
  - incremental scan: only files changed since the baseline commit
  - SARIF output so findings land as code scanning annotations
  - scan diff: two runs compared, delta dollars per call site
  - monorepos: several roots, one baseline each, one summary
  - fleet mode: many repos scanned locally, one rollup report
- Evals
  - templates for google, mistral, cohere, deepseek, xai, and groq
  - draft prompts.jsonl from string literals near the call site, local
    only
  - report latency and cost per call next to agreement, plus the cost
    of the eval itself
  - sample size guidance: say when the prompt set is too small to mean
    anything
  - embedding evals against a labeled pairs file, recall at k
  - optional judge model for open ended outputs; user picks it and
    pays for it
- Renderers
  - single file HTML report
  - CSV of findings for spreadsheets
  - a one line summary mode for CI logs
- Distribution and speed
  - Homebrew tap, winget, and scoop
  - a small docker image for CI runners
  - nix flake
  - build provenance attestation on releases
  - Aho-Corasick for the dictionary scan on big repos
  - parallel file walk, with a benchmark in CI to keep both honest

Never: hosted service, telemetry, model calls from the scanner.

### Scope for v1

In: the scan, the CI guard with its baseline ratchet, the public
catalog, generated eval scripts.

Was out for v1, shipped in v1.5: PR comments, the price-diff job, real
parsing for TypeScript. Still out: a built-in eval runner, editor
plugins, Homebrew. Hosted services, telemetry, and model calls from
the scanner are out for good.

## Exit codes

`0` clean, or every finding is baselined. `1` findings that violate the
failure policy. `2` operational error. They are never conflated, so CI can
script against them.

## License

MIT. See [LICENSE](LICENSE).
