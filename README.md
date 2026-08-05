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

`overwater scan` still exits 2 on purpose. It wires up when the renderers
land, and the renderers must reproduce the goldens byte for byte.

### Timeline

| Step | What | State |
|---|---|---|
| 1 | CLI scaffold, exit code contract, CI | done |
| 2 | Catalog: schema, validator, seeds, embedded snapshot | done |
| 3 | Fixture repos and golden verdicts | done |
| 4 | Detection layers 1 to 3 | done |
| 5 | Archetype classifier and rules engine | done |
| 6 | Renderers (terminal, MODELS.md, --json) and the golden harness | next |
| 7 | CI guard: exit codes, baseline ratchet, --fail-on | planned |
| 8 | Composite GitHub Action and dogfood workflow | planned |
| 9 | Catalog fetch with local cache, stale warning, --offline | planned |
| 10 | eval subcommand generating A/B scripts per finding | planned |

Steps 1 through 5 landed 2026-08-05.

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
