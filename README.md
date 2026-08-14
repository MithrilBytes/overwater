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
sh install.sh v2.4.1
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
release. Use it rather than an exact version: the Action downloads a
release binary checked against a sha256 written into `action.yml`, and
those checksums cannot exist until that release has built, so a tag
never carries its own pin. The release workflow writes the pin
afterwards and moves `v2` to it.

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

Each release then pins itself. The workflow that built it downloads its
own `SHA256SUMS`, writes the checksums into `action.yml` and the package
manifests, commits that to `main`, and moves the floating `v2` tag to
it. Nothing about a release is done by hand.

A price change used to carry a fourth component, `v2.2.1.1`, so a
catalog refresh would read differently from a code change. It does read
differently, but the tag is not where that belongs: the release notes
are, and they already say so. Four component tags are not semver, so
every one of them needed a twin pushed at the same commit for
`go install` to resolve, which meant two tags per price change and a
rule about which lane owned the patch number. None was ever cut before
the scheme came out.


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

**v2.4** is the current line, and it came out of pointing the scanner at
128 real public repositories instead of fixtures.

Layer 2 had been treating any catalog id in any file as a call, and it
was wrong far more often than the corpus showed. Comments, docstrings,
documentation, test fixtures and configuration rosters no longer count
as calls, a hyphenated identifier no longer matches a model inside
itself, agent tooling that borrows a model's family name is not a model,
and matching is case insensitive because providers ship ids like
Qwen3-VL and people write GPT-4o. Rerunning the same 128 repositories:
of the ones that call no LLM, 19 reported findings before and 9 do now,
and the phantom findings among them fell from 2,283 to 147.

What cannot be priced is now said out loud rather than passing as clean.
A model the catalog does not carry is named on stderr as unpriced, with
the `catalog diff -reverse` command that looks it up; an HTTP call whose
model is a runtime variable, and `claude --print`, are reported as calls
with no model to price.

A monorepo scan went from 242 seconds to 8: `traceConfigModels` was
rebuilding seven regexes and rereading every file for each config key.
Peak memory fell with it, since the analyzer had been holding two copies
of the repository.

The ratchet survives a rename, the corpus carries seventeen cases lifted
from real repositories, every rule has an end to end fixture, and a
tripwire now has a form a generated eval can exit on.

Releasing changed too. Tags went back to plain semver, and the Action
pins itself: its checksums cannot exist until the release has built, so
for three releases `action.yml` described the one before it and an
automatic price update repinned nothing at all. `@v2` now floats to
whichever commit pins the current release.

Verified by 291 labeled corpus cases at 0.97 accuracy on a 95 case
holdout split assigned before tuning, 103 black box smoke checks through
the real binary, byte for byte golden output for six fixtures covering
every rule, thirteen metamorphic properties, fuzz targets over the
parsers, and a gate that fails CI when analysis time grows faster than
its input.

## Next

### Speed

The numbers that ordered this list predate parallel analysis and the
quadratic fixes in v2. Profile before optimizing.

Aho-Corasick was measured and declined. A working automaton over the
182 catalog keys, producing byte identical findings, bought 36ms of a
173ms scan. The key loop is 14 percent of scan CPU on a repository that
calls no LLM, 7 percent on a model dense one and 1 percent on a
monorepo, and `unknownModelRE` costs more than the loop on the same
lines. The dictionary is not the expensive half of layer 2.

- [ ] `traceConfigModels` is 83 percent of a 242 second scan of
      microsoft/vscode, `regexp.MustCompile` alone 30 percent of it. It
      compiles up to seven regexes per file per config key and walks the
      repository once per config line. This is the speed item.
- [ ] cap peak memory: 854MB on 16,888 files and 170MB of source, 5.0x
      walked bytes, from 1.16GB and 6.8x measured back to back on the
      same tree at the same wall clock. The three cheap copies are gone:
      the walker hands its strings to `byPath` instead of being copied
      out of, layer 2's mask view is built with the file and released
      with it rather than cached for the pass, and the line index is
      `int32` and presized. What is left is the two masked views every
      later stage comes back to and the fan in index, all read for the
      whole pass. Streaming is still the wrong lever, since resolution
      follows imports and fan in indexes every file.

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
- [x] build the container image for tags that price-release pushes

### Detection

- [x] corpus cases from real repositories, not written for the corpus.
      Seventeen of them took holdout accuracy from 0.99 to 0.97, which
      is the honest number: the rest were written to be classified.
- [x] schema field counts bound the output token estimate
- [x] machine readable tripwires that generated evals exit on
- [ ] measured tokens per call, read from the same usage exports
      `volumes import` already parses

Cost ranges instead of point estimates came off this list. The soft
number is tokens per call, and the only ceilings the scanner can read
are `max_tokens` and the context window, which price the `py-extraction`
finding anywhere from $3 to $50,256 a month against a point estimate of
$78. Anything narrower is a percentage someone chose. A volumes file
carries call counts, so measuring volume would not narrow a token range
either. Meanwhile the comparison the finding exists to make barely
moves: over a 25x swing in `default_input`, claude-opus-5 against
claude-haiku-4-5 goes from 4.83x to 5.07x while both dollar figures
triple. Measuring the tokens beats bracketing them.

### Guard

- [x] rename stable fingerprints; a moved file reads as moved
- [x] a sixth fixture covering the newer rules end to end

### Catalog

- [x] reverse diff: report models LiteLLM knows and we do not
- [x] query the dated price history from the CLI

Never: hosted service, telemetry, model calls from the scanner.

## License

MIT. See [LICENSE](LICENSE).
