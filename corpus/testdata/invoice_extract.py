from openai import OpenAI

client = OpenAI()

SCHEMA = {
    "type": "object",
    "properties": {
        "vendor": {"type": "string"},
        "total_cents": {"type": "integer"},
        "due_date": {"type": "string"},
    },
    "required": ["vendor", "total_cents"],
}


def extract_invoice(text: str) -> dict:
    resp = client.chat.completions.create(
        model="gpt-5-mini",
        temperature=0,
        max_tokens=300,
        response_format={"type": "json_schema", "json_schema": {"name": "invoice", "schema": SCHEMA}},
        messages=[{"role": "user", "content": text}],
    )
    return resp.choices[0].message.content
