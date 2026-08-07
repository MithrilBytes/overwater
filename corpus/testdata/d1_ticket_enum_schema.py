from openai import OpenAI

client = OpenAI()

SCHEMA = {
    "type": "json_schema",
    "json_schema": {
        "name": "ticket_label",
        "schema": {
            "type": "object",
            "properties": {"label": {"type": "string", "enum": ["billing", "bug", "how_to", "abuse"]}},
            "required": ["label"],
        },
    },
}


def label_ticket(body: str) -> str:
    response = client.chat.completions.create(
        model="gpt-4.1-nano",
        temperature=0,
        max_tokens=20,
        response_format=SCHEMA,
        messages=[
            {"role": "system", "content": "Pick the single label that best fits the ticket."},
            {"role": "user", "content": body},
        ],
    )
    return response.choices[0].message.content
