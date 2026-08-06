# Model catalog

One YAML file per model under `models/`. Each entry records the id, the
provider, known aliases, prices in dollars per million tokens, the context
window, a capability tier (frontier, mid, small, embedding), the release
date, an optional deprecation date, and the source URL backing the pricing
claim.

To update a price: edit the entry, bump the date in `VERSION`, then run

```bash
go run ./cmd/overwater catalog build
```

and commit the YAML change together with the regenerated `catalog.json`.
CI rejects the change if `catalog.json` is out of sync with the entries.

The emitted `catalog.json` is embedded into the binary at build time and
published to GitHub Pages on merge, at

    https://mithrilbytes.github.io/overwater/catalog.json

That static file is the entire endpoint: no server, no database.
`overwater catalog refresh` pulls it into a local cache, and the scanner
prefers the cache only when it is newer than the embedded snapshot.

## What belongs in the catalog

Models people actually reference in code, priced by the provider's own
API, with a source URL that backs the numbers. Deprecated and retired
ids belong here too: they are what the deprecated-model rule detects,
and the deprecation date is the finding.

Open weight models served by many hosts (the Llama family, for example)
are deliberately absent, because a price there belongs to the host, not
the model. An entry for a specific hosted offering with its own pricing
page is welcome; a made-up consensus price is not. Unknown model strings
are still reported by the scanner at low confidence, so absence from the
catalog never means silence.
