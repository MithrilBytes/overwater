# Overwater verdict

Prices from catalog 2026-08-06. Costs are estimates at 10,000 calls per
month per call site; override with --volume.

```
Call site: app/api/chat/route.ts:7 (chat: streaming, no max_tokens; medium confidence)
Current:   claude-opus-5 at ~$126/mo at estimated volume
Candidate: same model with a max_tokens cap; cost unchanged until a response runs long
Tripwire:  None; a cap only fires when a response tries to exceed it
Flag:      No max_tokens set; worst case spend is unbounded
```

```
Call site: src/classify.ts:57 (classification: temp 0, JSON schema; high confidence)
Current:   claude-opus-5 at ~$135/mo at estimated volume
Candidate: claude-haiku-4-5, same capability tier for this task class, ~$27/mo
Tripwire:  If eval agreement drops below 97%, stay put
Flag:      No prompt caching on a 1,191-token repeated system prompt
```
