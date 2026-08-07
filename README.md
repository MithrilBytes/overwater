# overwater

overwater finds LLM call sites where the model is bigger, pricier, or
more wastefully configured than the task in front of it. It reads your
code, prices every call against a public model catalog, and nominates
cheaper candidates in dollars per month. In CI it fails builds that
introduce new waste and leaves the waste you already had alone.

The scanner never transmits your code. It makes no model API calls. The
only network request it can make is fetching the catalog, and a catalog
snapshot ships inside the binary, so it works with the network off.

## Install

```bash
go install github.com/MithrilBytes/overwater/cmd/overwater@latest
```

Release binaries for macOS, Linux, and Windows ship with a SHA256SUMS
file. The installer verifies it for you:

```bash
curl -fsSLO https://raw.githubusercontent.com/MithrilBytes/overwater/main/scripts/install.sh
sh install.sh v2.2.0
```

## Usage

```bash
overwater scan path/to/repo
overwater scan path/to/file.py
```

A path is a directory or a single file. A file is scanned with its
containing directory as context, so imports and wrapper defaults
resolve as they do in a whole repo scan, and that directory's
`.overwater.yaml` applies; only the named file reports findings.

Every finding renders five fields, in this order:

```
Call site: src/classify.ts:57 (classification: temp 0, JSON schema; high confidence)
Current:   claude-opus-5 at ~$135/mo at estimated volume
Candidate: claude-haiku-4-5, same capability tier for this task class, ~$27/mo
Tripwire:  If eval agreement drops below 97%, stay put
Flag:      No prompt caching on a 1,191-token repeated system prompt
```

When nothing clears the confidence bar the whole verdict is: `Keep the
models you have.`

Findings are nominations, not directives. Only an eval can prove a
cheaper model safe for your task, so every finding carries the
condition under which you should not switch.

### Commands

| Command | Does |
|---|---|
| `scan` | report call sites in one or more repositories |
| `diff` | compare two `scan --json` reports, with delta dollars |
| `fleet` | scan a list of repositories into one rollup |
| `eval` | generate a runnable A/B script per finding |
| `volumes` | import a provider usage export into a volumes file |
| `catalog` | show, build, refresh, or diff the model catalog |
| `explain` | print what a rule looks for and what it means |
| `version` | print the build |

### Scan flags

| Flag | Does |
|---|---|
| `--json`, `--html`, `--csv`, `--sarif`, `--summary` | output surfaces |
| `--models-md` | write MODELS.md into the scanned repo |
| `--baseline`, `--update-baseline` | the ratchet |
| `--fail-on new\|any\|none` | failure policy, default `new` |
| `--incremental` | scan only files changed since the baseline commit |
| `--max-baseline-age-days` | nag about findings baselined too long |
| `--volume` | calls per month per call site, default 10,000 |
| `--volumes` | JSON file of measured monthly calls, by site or model |
| `--refresh`, `--offline` | catalog fetch, and forbidding it |

### Measured volumes

Without measured traffic every dollar figure is the default assumption
times a price, and says so: `at estimated volume`. Feed real numbers in
and the findings priced from them read `at measured volume` instead,
with `volume` and `volume_source` on each `--json` finding.

```json
{
  "sites": {"src/classify.ts:57": 250000},
  "models": {"gpt-5.1": 1200000}
}
```

```bash
overwater scan --volumes volumes.json path/to/repo
```

A site key is `file:line`, relative to the scan root. A model key
covers every call site on that model, split evenly, and loses to a site
key. Both beat an `overwater:volume` pragma. All three are counts of
one call site's own traffic, so fan in never multiplies them; it
multiplies the default assumption, `--volume`, and a config volume.
Keys that match no call site are named on stderr; a malformed file is
exit 2.

Provider usage exports become a volumes file keyed by model:

```bash
overwater volumes import -o volumes.json usage.csv
```

Both a CSV with a model column and a request count column and a JSON
array of records work; the columns are matched by name, and which ones
were read and how many rows print on stderr. Models the catalog does
not carry are reported and kept. The file is read locally; overwater
never calls a provider API.

### CI

Record a baseline once, commit it, and only new findings fail:

```yaml
- uses: actions/checkout@v4
- uses: MithrilBytes/overwater@v2.2.0
  with:
    baseline: .overwater.json
```

Add `pr-comment: true` for a sticky verdict comment, which needs
`pull-requests: write`. Add `incremental: true` on PR builds.

Exit codes are `0` clean, `1` findings under the failure policy, `2`
operational error. They are never conflated.

### Per repo config

```yaml
# .overwater.yaml
volume: 40000
budget_monthly_usd: 2500
disable: [uncached-system-prompt]
thresholds:
  retry-amplification:
    min_retries: 5
```

A config applies to its own repository and no other, in any argument or
list order.

### Pragmas

```ts
// overwater:archetype=summarization
// overwater:volume=250000
// overwater:ignore
```

## How it reads a call site

Four layers feed a rules engine.

1. **Manifests.** `package.json`, `requirements.txt`, `pyproject.toml`,
   `go.mod`, `Gemfile`, `pom.xml`, `build.gradle`, `*.csproj`,
   `composer.json`, `Cargo.toml`, `Package.swift`.
2. **Model strings.** The catalog is the dictionary: 88 entries across
   15 providers, matched by id and alias. Strings that look like models
   but are not in the catalog are reported at low confidence.
3. **Call shape.** Temperature, token caps, schemas, tools, streaming,
   reasoning effort, retries, image detail, embedding dimensions,
   cache control, system prompt size. JavaScript, TypeScript, Python,
   Go, Ruby, C#, PHP, shell, and notebooks parse as key value pairs;
   Java, Kotlin, Rust, Scala, and Swift parse as builder chains. Signals
   come from the call's own bracket balanced extent over comment and
   prose masked source, so a neighboring call cannot bleed in and a
   prompt that merely mentions temperature cannot fake one. Prompts and
   schemas referenced by name resolve across imports and tsconfig
   aliases. An unreadable shape reports as unreadable.
4. **Archetype.** Extraction, classification, summarization, chat,
   agentic, embedding, translation, reranking, moderation,
   transcription, vision, codegen. Scored from the enclosing function
   name, the resolved prompt, the SDK method called, schema semantics,
   and the token cap read as intent. A phrase the prompt negates does
   not score. Below the evidence floor the answer is unknown rather than
   a guess, and confidence is graded so a finding leaning on a narrow
   call is demoted.

Call sites also carry fan in, the number of places that call the
enclosing function, and the models those callers pass. A helper
wrapping the SDK reports as one site with many callers rather than as
one call, and is priced for all of them: the volume multiplies by the
caller count, the finding reads `across 8 callers`, and `--json`
carries `"volume_source": "fan-in"` and `"callers"`. Only an exactly
resolved count multiplies; a name defined twice, or a helper with no
visible caller, stays at one. A helper whose model is a parameter is
priced for the callers that take its default, since the rest pass a
model of their own and are priced at the sites where they write it.

Every rule, threshold, and price lives in `rules/*.yaml` and `catalog/`.
The engine holds no numbers.

### Rules

`frontier-extraction`, `uncached-system-prompt`, `unbounded-max-tokens`,
`batch-on-realtime`, `pricey-embeddings`, `deprecated-model`,
`effort-overkill`, `retry-amplification`, `hot-temperature-extraction`,
`uncapped-embedding-dimensions`, `image-detail-high`,
`duplicate-call-sites`.

## Catalog

One YAML file per model, validated and emitted as a single versioned
`catalog.json` that is embedded in the binary and served at
[catalog.json](https://raw.githubusercontent.com/MithrilBytes/overwater/main/catalog/catalog.json),
mirrored on GitHub Pages. Entries carry prices, context window,
capability flags, cache and batch rates, release and deprecation dates,
and a source URL. Retired models stay in the catalog because legacy code
still references them.

Contributors update a price by editing one file. A nightly job diffs the
catalog against LiteLLM and opens a PR when a provider moves a price.

## Releases

**v1** shipped the tool: catalog, four detection layers, rules engine,
four renderers, the baseline ratchet, the GitHub Action, catalog fetch
with `--offline`, and generated eval scripts.

**v1.5** added structural parsing for TypeScript, a labeled corpus with
an accuracy floor, the nightly price watch, and PR comments.

**v2.0** added nine parsed language families plus notebooks and shell,
six more manifests, config tracing for env vars and Azure deployment
names, six more archetypes, twelve rules, exact cache pricing, per repo
config, baseline aging, incremental scans, `diff`, `fleet`, multi root
scans, HTML, CSV, SARIF, and summary output, eight provider eval
templates with an optional judge, and 88 catalog entries.

**v2.1** added measured volumes: a volumes file keyed by call site or
model, an importer for provider usage exports, and provenance on every
dollar figure. A rebuilt archetype scorer that reads the SDK method, the
token cap as intent, schema shape, and negation, so a prompt saying
"never reply to the customer" no longer scores as a chat. Call sites
gained fan in and the models their callers pass.

**v2.2** is the current line. A helper that wraps the SDK is now priced
for the callers it serves, not as one call: a wrapper with two hundred
callers costs two hundred times a leaf call. Only an exactly resolved
helper multiplies, and a caller that passes its own model is billed to
itself rather than twice. A scan root may be a single file, for editors
and pre commit hooks. `explain <rule-id>` prints a rule from its own
YAML.

Verified by 274 labeled corpus cases at 0.99 accuracy on a holdout split
assigned before any tuning, 88 black box smoke checks through the real
binary, byte for byte golden output for five fixture repositories, fuzz
targets over the parsers, a files per second benchmark, and a scaling
gate that fails CI when analysis time grows faster than the input.

## Next

### Speed

The numbers that ordered this list predate parallel analysis and the
quadratic fixes in v2. Profile before optimizing.

- [ ] Aho-Corasick over the dictionary, if a profile still says so
- [ ] cap peak memory on monorepo scans; stream instead of holding
      every file

### Deploy

Manifests for Homebrew, scoop, and winget, a nix flake, a distroless
image on GHCR, release notes built from the commit log, and build
provenance on every released binary all ship as of v2.2. What is left:

- [ ] publish the Homebrew formula to a tap repository

### Detection

- [ ] corpus cases from real repositories, not written for the corpus
- [ ] cost ranges instead of point estimates
- [ ] schema field counts bound the output token estimate
- [ ] machine readable tripwires that generated evals exit on

### Guard

- [ ] rename stable fingerprints; a moved file should read as moved
- [ ] a sixth fixture covering the newer rules end to end

### Catalog

- [ ] reverse diff: report models LiteLLM knows and we do not
- [ ] query the dated price history from the CLI; the snapshots are
      already written on every price change

Never: hosted service, telemetry, model calls from the scanner.

## License

MIT. See [LICENSE](LICENSE).
