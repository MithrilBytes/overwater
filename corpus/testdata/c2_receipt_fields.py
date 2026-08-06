import anthropic

client = anthropic.Anthropic()

RECEIPT_TOOL = {
    "name": "record_receipt",
    "description": "Store the fields found on a scanned receipt.",
    "input_schema": {
        "type": "object",
        "properties": {
            "merchant": {"type": "string"},
            "total_cents": {"type": "integer"},
            "purchased_at": {"type": "string"},
        },
        "required": ["merchant", "total_cents"],
    },
}


def parse_receipt(scanned_text: str) -> dict:
    message = client.messages.create(
        model="claude-haiku-4-5",
        max_tokens=300,
        tools=[RECEIPT_TOOL],
        tool_choice={"type": "tool", "name": "record_receipt"},
        messages=[{"role": "user", "content": scanned_text}],
    )
    return message.content[0].input
