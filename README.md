# overwater

overwater finds LLM call sites where the model is bigger, pricier, or
more wastefully configured than the task in front of it. It reads your
code, prices every call against a public model catalog, and nominates
cheaper candidates in dollars per month. In CI it fails builds on new
findings and passes the ones already recorded in a baseline.

The scanner transmits no code and calls no model API. Its only network
request is fetching the catalog, and a snapshot ships inside the
binary, so it runs with the network off.

Project page: https://mithrilbytes.github.io/overwater/

## Install

```bash
go install github.com/MithrilBytes/overwater/cmd/overwater@latest
```

Release binaries for macOS, Linux, and Windows ship with a SHA256SUMS
file and a signed build provenance attestation. The installer checks the
digest, and the attestation when `gh` is available:

```bash
curl -fsSLO https://raw.githubusercontent.com/MithrilBytes/overwater/main/scripts/install.sh
sh install.sh v2.8.0
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
Current:   claude-opus-5 at ~$91/mo at estimated volume
Candidate: claude-haiku-4-5, same capability tier for this task class, ~$18/mo
Tripwire:  If eval agreement drops below 97%, stay put
Flag:      No prompt caching on a 1,191-token repeated system prompt
```

When nothing clears the confidence bar the whole verdict is: `Keep the
models you have.`

Findings are nominations. A scanner cannot know a cheaper model is safe
for your task, so every finding states the condition under which you
should not switch, and `overwater eval` generates the A/B script that
tests it.

That condition ships in both forms. The sentence is what the terminal
prints; `--json` carries the same condition as numbers, and the
generated script exits on them:

```json
"tripwire": "If eval agreement drops below 97%, stay put",
"tripwire_check": {"metric": "agreement", "compare": "below", "threshold": 97}
```

A script exits 0 when the tripwire held, 1 when it tripped, and 2 when
the run could not answer, so CI reads the exit code rather than the
report. A tripwire that names nothing an eval can measure, such as a
retired model id, carries no `tripwire_check` and gates on nothing.

### Commands

| Command | Does |
|---|---|
| `scan` | report call sites in one or more repositories |
| `diff` | compare two `scan --json` reports, with delta dollars |
| `fleet` | scan a list of repositories into one rollup |
| `eval` | generate a runnable A/B script per finding |
| `volumes` | import a provider usage export into a volumes file |
| `catalog` | show, build, refresh, or diff the catalog, and read its price history |
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
- uses: MithrilBytes/overwater@v2
  with:
    baseline: .overwater.json
```

`@v2` is a floating tag that moves to whichever commit pins the current
release, and an exact version such as `@v2.6.0` works too. It did not
always: the Action used to check the binary against a sha256 written
into `action.yml`, and a checksum cannot exist until the release has
built, so every tag shipped the previous release's digests and only the
floating tag resolved. The Action verifies build provenance instead,
which is signed on the release path and certifies which workflow
produced the bytes, so the only thing `action.yml` still pins is the
version, and a version is knowable before the tag. Releases from v2.6.0
on carry their own pin.

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
exclude: ["*.generated.ts", "reports/"]
thresholds:
  retry-amplification:
    min_retries: 5
```

A config applies to its own repository and no other, in any argument or
list order.

`exclude` takes path globs. A pattern without a slash matches any one
segment, so `*.json` reaches a model registry at any depth; a pattern
with a slash matches the whole path or names a directory and covers the
tree under it. It is the lever for a file that names model ids without
calling them and cannot carry a pragma, such as generated code, a
vendored fixture, or this tool's own reports. `disable` is the blunter
instrument: it turns a rule off across the whole repository.

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
   but are not in the catalog are named on stderr as unpriced.

   Only code calls a model. Documentation, test files and fixtures are
   read as context but report no call sites of their own: a README
   listing models, or a test asserting on one, spends nothing. In
   configuration the distinction is binding against roster, so
   `model: gpt-5-mini` is a call site and a list of models a user may
   pick from is not.

   Some calls spend tokens without naming a model: an HTTP call to
   `/chat/completions` whose model is a runtime variable, or a shell
   script running `claude --print`. Neither has a catalog entry to price
   against, so both are named on stderr as unpriced.
3. **Call shape.** Temperature, token caps, schemas, tools, streaming,
   reasoning effort, retries, image detail, embedding dimensions,
   cache control, system prompt size. JavaScript, TypeScript, Python,
   Go, Ruby, C#, PHP, shell, and notebooks parse as key value pairs;
   Java, Kotlin, Rust, Scala, and Swift parse as builder chains.
   Signals are read from the call's own bracket balanced extent, over
   source with comments and prose masked out. Prompts and schemas
   referenced by name resolve across imports and tsconfig aliases. An
   unreadable shape reports as unreadable.
4. **Archetype.** Extraction, classification, summarization, chat,
   agentic, embedding, translation, reranking, moderation,
   transcription, vision, codegen. Scored from the enclosing function
   name, the resolved prompt, the SDK method called, schema semantics,
   and the token cap read as intent. A negated phrase does not score.
   Below the evidence floor the archetype is unknown, and confidence is
   graded.

Call sites carry fan in, the number of places that call the enclosing
function, and the models those callers pass. A helper that wraps the
SDK is priced for its callers: volume multiplies by the caller count,
the finding reads `across 8 callers`, and `--json` carries
`"volume_source": "fan-in"` and `"callers"`. Only an exact count
multiplies. A name defined twice, or a helper with no visible caller,
stays at one. A helper whose model is a parameter is priced for the
callers that take its default; the rest are priced where they write
their own model.

Every rule, threshold, and price lives in `rules/*.yaml` and `catalog/`.
The engine holds no numbers.

### Rules

`batch-on-realtime`, `deprecated-model`, `duplicate-call-sites`,
`effort-overkill`, `frontier-agentic`, `frontier-extraction`,
`hot-temperature-extraction`, `image-detail-high`, `pricey-embeddings`,
`retry-amplification`, `thinking-budget-overkill`,
`unbounded-max-tokens`, `uncached-system-prompt`, `uncached-tool-loop`,
`uncapped-embedding-dimensions`.

## Catalog

One YAML file per model, validated and emitted as a single versioned
`catalog.json` that is embedded in the binary and served at
[catalog.json](https://raw.githubusercontent.com/MithrilBytes/overwater/main/catalog/catalog.json),
mirrored on GitHub Pages beside the project page in `site/`. Entries
carry prices, context window,
capability flags, cache and batch rates, release and deprecation dates,
and a source URL. Retired models stay in the catalog; legacy code still
references them, and the deprecated-model rule needs their dates.

Contributors update a price by editing one file. A nightly job diffs the
catalog against LiteLLM and opens a PR when a provider moves a price.

A price that moves together with the context window is reported as
repointed and never applied: upstream reuses an id when a family ships a
new generation, as `mistral-medium-3` did at 1.5/7.5 over a 262144
window while the entry here stayed at 0.4/2 over 131072.

The same file answers the other question. `catalog diff -reverse` lists
models LiteLLM prices that this catalog does not carry, collapsing the
routes upstream lists them under, so a scan that could not price
something becomes an entry to add:

```bash
overwater catalog diff -reverse -only deepseek-v4-flash litellm.json
```

A scan that meets a model it cannot price prints that command with the
ids filled in.

Every applied price change drops a dated snapshot of the whole catalog
into `catalog/history/`, and `catalog history` reads them back: the
snapshot list, one model's price over time, or what moved on one date.

```bash
overwater catalog history -model claude-opus-5
overwater catalog history -on 2026-08-08
```

## Releases

Tags are semver: `vMAJOR.MINOR.PATCH`, nothing else. Merging a
price-watch PR cuts the next patch and releases it on its own, so a
price the catalog accepts reaches the binaries without a human picking a
version.

Each release pins itself, in two halves that move at different times.
`action.yml` names the version and nothing else, so it is bumped and
committed before the tag; the release refuses to build if the tree it is
cutting names a different version than the tag. The package manifests
need digests, and a digest cannot exist until the artifacts do, so the
job that runs after the release writes those from the bytes it just
built, commits them to `main`, and moves the floating `v2` tag.

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
dollar figure. The archetype scorer was rebuilt around the SDK method,
the token cap read as intent, schema shape, and negation. Call sites
gained fan in and the models their callers pass.

**v2.2** priced a helper that wraps the SDK for its callers, so a
wrapper called two hundred times costs two hundred leaf calls. It added
packaging: Homebrew, scoop, and winget manifests, a nix flake, a
distroless image on GHCR, release notes from the commit log, and build
provenance on every binary. A scan root may be a single file, and
`explain <rule-id>` prints a rule from its own YAML.

**v2.3** made merging a price-watch PR tag and release on its own, so a
price the catalog accepts reaches the binaries without a human picking a
version. Applying a price carries the cache rates with it, which
providers publish as multiples of base input and the previous version
left describing the old price.

**v2.8** generates the facts in this file and on the project page from
the repository with `tools/sync-docs`: the rule list, and the release
the install examples name. A workflow runs it on main and a test checks
it on a pull request. The automated price release bumps the Action
before it tags.

**v2.7** holds back a price that arrives with a new context window,
which is upstream reusing an id rather than repricing a model. The
reasoning estimate reads a pinned effort or a stated thinking budget
before falling back on the assumption in `estimates.yaml`. Dogfood
builds the tree it tests.

**v2.6** verifies build provenance instead of a checksum committed into
`action.yml`. A digest cannot exist before the artifacts are built, so
earlier tags shipped the previous release's digests. From v2.6.0 a tag
names its own release and an exact version pins.

**v2.5** came out of auditing the tool against itself. Config values and
source lines are no longer printed on stderr, a scanned filename is
escaped before it reaches a report, a symlinked scan root resolves,
dotted Bedrock ids are read, the structural parser keeps a temperature
the regex layer recovered, and reasoning models are priced for their
thinking. Three rules were added, the Action's defaults no longer fail
every run, and layer 1 reports a declared SDK with no call site.

**v2.4** came out of pointing the scanner at 128 real public
repositories instead of fixtures. Comments, docstrings, documentation,
test fixtures and configuration rosters no longer count as calls, a
hyphenated identifier no longer matches a model inside itself, and
matching is case insensitive. Of the repositories that call no LLM, 19
reported findings before and 9 do now, and the phantom findings among
them fell from 2,283 to 147. A model the catalog does not carry, an
HTTP call whose model is a runtime variable, and `claude --print` are
named on stderr as unpriced. A monorepo scan went from 242 seconds to 8.
The ratchet survives a rename, the corpus carries seventeen cases lifted
from real repositories, every rule has an end to end fixture, and a
tripwire has a form a generated eval can exit on.

Verified by 291 labeled corpus cases at 0.97 accuracy on a 95 case
holdout split assigned before tuning, 103 black box smoke checks through
the real binary, byte for byte golden output for six fixtures covering
every rule, thirteen metamorphic properties, fuzz targets over the
parsers, and a gate that fails CI when analysis time grows faster than
its input.

## Next

### Speed

- [x] `traceConfigModels` no longer rereads the repository per config key
- [ ] cap peak memory: 854MB on 16,888 files and 170MB of source

### Deploy

Manifests for Homebrew, scoop, and winget, a nix flake, a distroless
image on GHCR, release notes built from the commit log, and build
provenance on every released binary all ship as of v2.2.

The three manifests are generated on every release and published
nowhere, so `brew install overwater` and its equivalents do not work.

- [x] build the container image for tags that price-release pushes

### Detection

- [x] corpus cases from real repositories, not written for the corpus
- [x] schema field counts bound the output token estimate
- [x] machine readable tripwires that generated evals exit on
- [x] read the reasoning spend a call states before assuming one
- [ ] measured tokens per call for everything else, read from the same
      usage exports `volumes import` already parses

### Guard

- [x] rename stable fingerprints; a moved file reads as moved
- [x] a sixth fixture covering the newer rules end to end

### Catalog

- [x] reverse diff: report models LiteLLM knows and we do not
- [x] query the dated price history from the CLI

Never: hosted service, telemetry, model calls from the scanner.

## License

MIT. See [LICENSE](LICENSE).
