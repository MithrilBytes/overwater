from openai import OpenAI

client = OpenAI()

ROUTE_SCHEMA = {
    "type": "object",
    "properties": {"queue": {"type": "string", "enum": ["billing", "outage", "how_to", "abuse"]}},
    "required": ["queue"],
    "additionalProperties": False,
}


def triage_ticket(body: str) -> str:
    resp = client.chat.completions.create(
        model="gpt-5-nano",
        response_format={"type": "json_schema", "json_schema": {"name": "route", "schema": ROUTE_SCHEMA, "strict": True}},
        messages=[
            {"role": "system", "content": "Route each support ticket to exactly one queue."},
            {"role": "user", "content": body},
        ],
    )
    return resp.choices[0].message.content
