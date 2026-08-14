"""Claims triage. One frontier model doing four jobs it does not need to.

The sixth fixture exists because seven rules had no end to end coverage:
they were unit tested and never once run through the whole pipeline into
a golden. Each call below is written to trip exactly one of them, in the
shape the rule was written for rather than a minimal stub.
"""

import anthropic

from policy import TRIAGE_POLICY

client = anthropic.Anthropic()


def classify_note(note: str):
    """Extraction at a temperature, with reasoning effort it cannot use.

    Trips hot-temperature-extraction, since an extraction that has to
    agree with itself run to run has no business above zero, and
    effort-overkill, since pulling four fields out of a note is not a
    reasoning problem. The uncached system prompt is shared with every
    other call in this file and never marked, so it is billed in full
    each time.
    """
    return client.messages.create(
        model="claude-opus-5",
        system=TRIAGE_POLICY,
        temperature=0.7,
        max_tokens=512,
        tools=[
            {
                "name": "record_claim",
                "input_schema": {
                    "type": "object",
                    "properties": {
                        "claim_id": {"type": "string"},
                        "queue": {"type": "string"},
                        "severity": {"type": "string"},
                        "adjuster": {"type": "string"},
                    },
                },
            }
        ],
        tool_choice={"type": "tool", "name": "record_claim"},
        extra_body={"reasoning_effort": "high"},
        messages=[{"role": "user", "content": note}],
    )


def retry_note(note: str):
    """The same call again, with a retry budget on a frontier model.

    Trips retry-amplification: eight attempts against the dearest model
    in the catalog turns one timeout into eight bills. It also trips
    duplicate-call-sites, being identical in shape to classify_note.
    """
    return client.messages.create(
        model="claude-opus-5",
        system=TRIAGE_POLICY,
        temperature=0.7,
        max_tokens=512,
        max_retries=8,
        messages=[{"role": "user", "content": note}],
    )
