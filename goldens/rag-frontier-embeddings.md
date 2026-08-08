# Overwater verdict

Prices from catalog 2026-08-08. Costs are estimates at 10,000 calls per
month per call site; override with --volume.

```
Call site: ingest.py:8 (embedding: embeddings API; high confidence)
Current:   text-embedding-3-large at ~$13/mo at estimated volume
Candidate: text-embedding-3-small, same provider at the standard embedding tier, ~$2/mo
Tripwire:  If retrieval quality drops on your eval set, stay put
Flag:      No dimensions parameter on a model that supports one; vectors ship at full width
```
