from openai import OpenAI

client = OpenAI()

# Builds the invoice summary card for the billing dashboard.
CARD_FIELDS = {
    "type": "object",
    "properties": {
        "vendor": {"type": "string"},
        "amount_due_cents": {"type": "integer"},
        "due_date": {"type": "string"},
        "po_number": {"type": "string"},
    },
    "required": ["vendor", "amount_due_cents"],
}


def summarize_invoice(pdf_text: str) -> str:
    resp = client.chat.completions.create(
        model="gpt-5-mini",
        temperature=0,
        response_format={"type": "json_schema", "json_schema": {"name": "card_fields", "schema": CARD_FIELDS}},
        messages=[{"role": "user", "content": "Pull each field from the invoice text.\n\n" + pdf_text}],
    )
    return resp.choices[0].message.content
