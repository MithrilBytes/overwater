# Classifier corpus

Labeled call sites the archetype classifier is measured against. One
known model reference per file; `labels.json` holds the expected
archetype and the split. `TestCorpusAccuracy` in `internal/scan`
computes accuracy per split and precision and recall per archetype, and
fails below the floors, so classifier changes ship with a number.

Most cases are hand written from documented SDK usage across Anthropic,
OpenAI (chat completions and responses), the Vercel AI SDK, Google
GenAI, Cohere, Mistral, Bedrock, Azure, LangChain, and litellm, in
every language the scanner parses. They include traps on purpose:
misleading function names, prompts that state a different task than the
name, nested config objects, clients built far from the prompt.

## Cases from real repositories

The `r1_` to `r5_` files are lifted from public repositories that call
an LLM for their own reasons, not written for this corpus. A hand
written case exercises a rule; these do not know the rules exist, which
is why they fail differently.

    r1_  Kitware/vtk-mcp
    r2_  biztex/AI_vita
    r3_  basicallysource/sorter-v2
    r4_  lilfetz22/audio-digest-hub
    r5_  hexlet-basics/hexlet-basics

Each file names its source path in a comment at the top and keeps the
shape of the original call: the prompt as written, the caps and
temperature where the repository put them, the model where the
repository set it. Unrelated branches are dropped so one model string
is left. A model id the catalog does not price is replaced by the
nearest one it does, since an unpriced site is not a site; the comment
says so where it happened. Prompts stay in their own language, with
dashes flattened to keep the repository's own style rule.

## The labeling rule

Write the case first from a real usage pattern, then label it by
reading what the prompt asks the model to DO, the way a reviewer would.

- Never label a case by what the classifier says.
- Never adjust a label to make a test pass.
- If two archetypes both genuinely fit, do not add the case. The corpus
  is for cases a competent human would agree on.

Conventions that keep the hard middles consistent:

- The output decides it. A prompt that reads a thread and returns one
  label is classification, not summarization; a prompt that reads a
  transcript and returns prose is summarization, not transcription.
- An image or audio input decides it. Reading fields off a photo is
  vision, not extraction.
- A policy or safety verdict is moderation. An arbitrary taxonomy is
  classification.
- Generating an answer for a person to read is chat, including RAG
  answers. Tools the model chooses between is agentic; one tool forced
  to fill a schema is extraction.

## Splits

Every case carries `"split": "tune"` or `"split": "holdout"`, roughly
70/30, stratified by archetype. Tune cases are what a change may be
developed against. Holdout cases only measure it, and the test does not
print holdout misses unless `OVERWATER_SHOW_HOLDOUT` is set.

Add a case by adding a file and a label. Give it the split that keeps
its archetype near 30% holdout.

Cases added in a batch get their split from a rule fixed before the
batch is scored, so the holdout cannot be filled with the cases that
happen to pass: within an archetype, sort by filename and take the
first, and take none at all where that would push the archetype past
35% holdout.
