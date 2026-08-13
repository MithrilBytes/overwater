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
file. The installer verifies it for you:

```bash
curl -fsSLO https://raw.githubusercontent.com/MithrilBytes/overwater/main/scripts/install.sh
sh install.sh v2.3.0
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

Findings are nominations. A scanner cannot know a cheaper model is safe
for your task, so every finding states the condition under which you
should not switch, and `overwater eval` generates the A/B script that
tests it.

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
- uses: MithrilBytes/overwater@v2.3.1
  with:
    baseline: .overwater.json
```

The Action downloads a release binary pinned by checksum, and those
checksums only exist once the release is built. So a `MAJOR.MINOR.0`
tag always carries the previous release's pin, and the tag to use is
the `FIX` that follows it.

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
   but are not in the catalog are named on stderr as unpriced, so a
   model we do not carry cannot read as a clean bill of health.

   Only code calls a model. Documentation, test files and fixtures are
   read as context but report no call sites of their own: a README
   listing models, or a test asserting on one, spends nothing. In
   configuration the distinction is binding against roster, so
   `model: gpt-5-mini` is a call site and a list of models a user may
   pick from is not.

   Some calls spend tokens without naming a model: an HTTP call to
   `/chat/completions` whose model is a runtime variable, or a shell
   script running `claude --print`. Neither can be priced, because a
   price needs a catalog entry and neither a variable nor an agent CLI
   alias is one. They are named on stderr as unpriced rather than
   guessed at, so a repository that spends through one does not read as
   clean.
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

`frontier-extraction`, `uncached-system-prompt`, `unbounded-max-tokens`,
`batch-on-realtime`, `pricey-embeddings`, `deprecated-model`,
`effort-overkill`, `retry-amplification`, `hot-temperature-extraction`,
`uncapped-embedding-dimensions`, `image-detail-high`,
`duplicate-call-sites`.

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

The same file answers the other question. `catalog diff -reverse` lists
models LiteLLM prices that this catalog does not carry, collapsing the
routes upstream lists them under, so a scan that could not price
something becomes an entry to add:

```bash
overwater catalog diff -reverse -only deepseek-v4-flash litellm.json
```

A scan that meets a model it cannot price prints that command with the
ids filled in.

## Releases

Tags read `MAJOR.MINOR.FIX`, plus a fourth `UPDATE` component when a
merged price change reships the binaries with a new catalog and no code
change. Merging a price-watch PR tags and releases that update on its
own.

Four component tags are not semver, so each update also pushes a semver
twin at the same commit for `go install` to resolve: `v2.2.1.1` ships
alongside `v2.2.2`. The two lanes advance independently, updates
counting from the last human release and twins owning the patch lane,
so cut features and fixes at a minor or major bump.


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

**v2.3** is the current line. Merging a price-watch PR now tags and
releases on its own, so a price the catalog accepts reaches the
binaries without a human picking a version. Applying a price carries
the cache rates with it, which providers publish as multiples of base
input and the previous version left describing the old price.

Layer 2 was then measured against 128 real public repositories rather
than fixtures, and it was wrong far more often than the corpus showed:
19 of the 25 that call no LLM at all still reported call sites. It had
been treating any catalog id in any file as a call. Comments,
docstrings, documentation, test fixtures and configuration rosters no
longer are, a hyphenated identifier no longer matches a model inside
itself, and agent tooling that borrows a model's family name is not a
model. One configuration repository dropped from 1,364 findings to 33,
and a CLI that never opens a socket from 220 to none. No repository
that genuinely calls an LLM lost a finding.

Verified by 274 labeled corpus cases at 0.99 accuracy on an 88 case
holdout split assigned before tuning, 89 black box smoke checks through
the real binary, byte for byte golden output for five fixtures, fuzz
targets over the parsers, and a gate that fails CI when analysis time
grows faster than its input.

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
provenance on every released binary all ship as of v2.2. The three
manifests are generated into this repository and nothing publishes
them, so `brew install overwater`, `scoop install overwater`, and
`winget install MithrilBytes.Overwater` all fail today. What is left:

- [ ] publish the Homebrew formula to a tap repository
- [ ] publish the scoop manifest to a bucket repository
- [ ] submit the winget manifests to microsoft/winget-pkgs
- [ ] build the container image for tags that price-release pushes;
      `image.yml` runs on tag push, and a tag pushed with
      `GITHUB_TOKEN` starts no workflow, so an update ships binaries
      with no matching image

### Detection

- [ ] corpus cases from real repositories, not written for the corpus
- [ ] cost ranges instead of point estimates
- [ ] schema field counts bound the output token estimate
- [ ] machine readable tripwires that generated evals exit on

### Guard

- [ ] rename stable fingerprints; a moved file should read as moved
- [ ] a sixth fixture covering the newer rules end to end

### Catalog

- [x] reverse diff: report models LiteLLM knows and we do not
- [ ] query the dated price history from the CLI; the snapshots are
      already written on every price change

Never: hosted service, telemetry, model calls from the scanner.

## License

MIT. See [LICENSE](LICENSE).
