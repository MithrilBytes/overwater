from openai import OpenAI

client = OpenAI()

SCHEMA = {
    "type": "json_schema",
    "json_schema": {
        "name": "action_items",
        "schema": {
            "type": "object",
            "properties": {
                "owner": {"type": "string"},
                "task": {"type": "string"},
                "due_date": {"type": "string"},
            },
        },
    },
}


def action_items(transcript: str) -> str:
    """Pulls the commitments out of a meeting transcript; the recap is written elsewhere."""
    response = client.chat.completions.create(
        model="gpt-4.1-mini",
        temperature=0,
        max_tokens=600,
        response_format=SCHEMA,
        messages=[
            {"role": "system", "content": "List each commitment someone made, with the owner and the date they said. Copy the wording, do not summarize the meeting."},
            {"role": "user", "content": transcript},
        ],
    )
    return response.choices[0].message.content
