"""Pulling the same four fields out of correspondence, scanned mail.

This module and its fax twin were copied from one another when the
fax intake was added, and neither author knew about the other. Both
run at a temperature, which costs correctness rather than money: an
extraction that disagrees with itself between runs is worse than one
that is merely expensive.
"""

import anthropic

from policy import TRIAGE_POLICY

client = anthropic.Anthropic()

LETTER_FIELDS = {
    "type": "object",
    "properties": {
        "policy_number": {"type": "string"},
        "date_of_loss": {"type": "string"},
        "claimant_name": {"type": "string"},
        "amount_claimed": {"type": "string"},
    },
}


def extract_letter(letter: str):
    """Extract the four fields a claim needs from the letter."""
    return client.messages.create(
        model="claude-opus-5",
        system=TRIAGE_POLICY,
        temperature=0.4,
        max_tokens=600,
        tools=[{"name": "record_letter", "input_schema": LETTER_FIELDS}],
        tool_choice={"type": "tool", "name": "record_letter"},
        messages=[{"role": "user", "content": letter}],
    )
