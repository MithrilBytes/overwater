# Overwater verdict

Prices from catalog 2026-08-08. Costs are estimates at 10,000 calls per
month per call site; override with --volume.

```
Call site: legacy/summarize-v1.js:7 (summarization; high confidence)
Current:   text-davinci-003 at ~$151/mo at estimated volume
Candidate: gpt-5-mini, current replacement in the same tier, ~$36/mo
Tripwire:  None; there is no configuration in which a retired model id keeps working
Flag:      Unavailable after 2024-01-04; a correctness bug, not just a cost one
```

```
Call site: src/summarize.js:12 (summarization: cron scheduled; medium confidence)
Current:   gpt-5.1 at ~$198/mo at estimated volume
Candidate: gpt-5.1 through the batch endpoint at half price, ~$99/mo
Tripwire:  If results are needed in under an hour, stay put
Flag:      None
```
