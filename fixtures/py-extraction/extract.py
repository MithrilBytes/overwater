import anthropic

client = anthropic.Anthropic()

INVOICE_TOOL = {
    "name": "record_invoice",
    "description": "Record structured fields from an invoice.",
    "input_schema": {
        "type": "object",
        "properties": {
            "vendor": {"type": "string"},
            "invoice_number": {"type": "string"},
            "total_cents": {"type": "integer"},
            "currency": {"type": "string"},
            "due_date": {"type": "string"},
        },
        "required": ["vendor", "invoice_number", "total_cents"],
    },
}


def extract_invoice(text: str) -> dict:
    message = client.messages.create(
        model="claude-opus-5",
        max_tokens=1024,
        temperature=0,
        system="Extract invoice fields exactly as written. Never guess a value.",
        tools=[INVOICE_TOOL],
        tool_choice={"type": "tool", "name": "record_invoice"},
        messages=[{"role": "user", "content": text}],
    )
    return message.content[0].input
