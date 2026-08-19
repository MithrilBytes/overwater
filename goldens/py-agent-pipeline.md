# Overwater verdict

Prices from catalog 2026-08-08. Costs are estimates at 10,000 calls per
month per call site; override with --volume.

```
Call site: damage.py:20 (vision: JSON schema; low confidence)
Current:   gpt-5.1 at ~$173/mo at estimated volume
Candidate: same model with image detail auto or low; thumbnails do not need high detail
Tripwire:  If small print in images stops parsing, stay put
Flag:      Image detail pinned to high; thumbnails do not need high detail
```

```
Call site: damage.py:60 (embedding: embeddings API; high confidence)
Current:   text-embedding-3-large at ~$13/mo at estimated volume
Candidate: text-embedding-3-small, same provider at the standard embedding tier, ~$2/mo
Tripwire:  If nearest neighbor agreement drops below 90%, stay put
Flag:      No dimensions parameter on a model that supports one; vectors ship at full width
```

```
Call site: intake_fax.py:30 (extraction: temp 0.4, JSON schema; high confidence)
Current:   claude-opus-5 at ~$249/mo at estimated volume
Candidate: claude-haiku-4-5, same capability tier for this task class, ~$50/mo
Tripwire:  If eval agreement drops below 97%, stay put
Flag:      Temperature above zero on extraction risks inconsistent output; a correctness issue before a cost one
Flag:      No prompt caching on a 2,480-token repeated system prompt
```

```
Call site: intake_mail.py:30 (extraction: temp 0.4, JSON schema; low confidence)
Current:   claude-opus-5 at ~$249/mo at estimated volume
Candidate: consolidate with the first identical call and share one cached result
Tripwire:  If the sites feed different runtime inputs, stay put
Flag:      Temperature above zero on extraction risks inconsistent output; a correctness issue before a cost one
Flag:      No prompt caching on a 2,480-token repeated system prompt
```

```
Call site: intake_mail.py:30 (extraction: temp 0.4, JSON schema; high confidence)
Current:   claude-opus-5 at ~$249/mo at estimated volume
Candidate: claude-haiku-4-5, same capability tier for this task class, ~$50/mo
Tripwire:  If eval agreement drops below 97%, stay put
Flag:      None
```

```
Call site: triage.py:27 (classification: temp 0.7, JSON schema; medium confidence)
Current:   claude-opus-5 at ~$192/mo at estimated volume
Candidate: same model at default effort; extraction and classification rarely need deliberate reasoning
Tripwire:  If eval agreement drops below 97%, stay put
Flag:      No prompt caching on a 2,480-token repeated system prompt
```

```
Call site: triage.py:27 (classification: temp 0.7, JSON schema; high confidence)
Current:   claude-opus-5 at ~$192/mo at estimated volume
Candidate: claude-haiku-4-5, same capability tier for this task class, ~$38/mo
Tripwire:  If eval agreement drops below 97%, stay put
Flag:      None
```

```
Call site: triage.py:59 (chat: temp 0.7; medium confidence)
Current:   claude-opus-5 at ~$249/mo at estimated volume
Candidate: same model with a lower retry cap, or a cheaper model on the retry path
Tripwire:  If the provider's error rate genuinely needs this many attempts, stay put
Flag:      max_retries 8 on a frontier model multiplies worst case spend
```

```
Call site: triage.py:59 (chat: temp 0.7; high confidence)
Current:   claude-opus-5 at ~$249/mo at estimated volume
Candidate: same model with cache_control on the system prompt, ~$137/mo
Tripwire:  None; caching changes cost, not output
Flag:      No prompt caching on a 2,480-token repeated system prompt
```
