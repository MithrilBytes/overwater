# Classifier corpus

Labeled call sites the archetype classifier is measured against. One
known model reference per file; `labels.json` holds the expected
archetype. A test in `internal/scan` computes accuracy and fails below
the floor, so classifier changes ship with a number.

Cases are hand written to mirror real API usage, including traps:
misleading comments, prompts that state a different task than the
function name, nested config objects. Add a case by adding a file and a
label.
