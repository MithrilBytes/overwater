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

Status: early development. The CLI routes three commands (scan, eval,
catalog); none of them are implemented yet.

## License

MIT. See [LICENSE](LICENSE).
