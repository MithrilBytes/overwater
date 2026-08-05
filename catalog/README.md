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
published as a static file, so the scanner never needs a server.
