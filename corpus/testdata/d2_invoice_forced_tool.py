import anthropic

client = anthropic.Anthropic()

INVOICE_TOOL = {
    "name": "record_invoice",
    "description": "Record the fields read off an invoice.",
    "input_schema": {
        "type": "object",
        "properties": {
            "vendor": {"type": "string"},
            "invoice_number": {"type": "string"},
            "total": {"type": "number"},
            "due_date": {"type": "string"},
        },
        "required": ["vendor", "total"],
    },
}


def read_invoice(text: str) -> dict:
    message = client.messages.create(
        model="claude-sonnet-4-5",
        max_tokens=500,
        temperature=0,
        tools=[INVOICE_TOOL],
        tool_choice={"type": "tool", "name": "record_invoice"},
        messages=[{"role": "user", "content": text}],
    )
    return message.content[0].input
