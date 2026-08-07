import litellm

SCHEMA = {
    "type": "json_schema",
    "json_schema": {
        "name": "party_fields",
        "schema": {
            "type": "object",
            "properties": {
                "buyer": {"type": "string"},
                "seller": {"type": "string"},
                "closing_date": {"type": "string"},
                "price": {"type": "number"},
            },
        },
    },
}


def read_parties(deed: str) -> str:
    response = litellm.completion(
        model="openai/gpt-4.1-mini",
        messages=[
            {"role": "system", "content": "Copy the named fields out of the deed. Never infer a missing party."},
            {"role": "user", "content": deed},
        ],
        response_format=SCHEMA,
        temperature=0,
        max_tokens=350,
    )
    return response.choices[0].message.content
