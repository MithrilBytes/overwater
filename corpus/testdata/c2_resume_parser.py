from openai import OpenAI

client = OpenAI()

RESUME_SCHEMA = {
    "type": "object",
    "properties": {
        "full_name": {"type": "string"},
        "years_experience": {"type": "number"},
        "skills": {"type": "array", "items": {"type": "string"}},
        "last_title": {"type": "string"},
    },
    "required": ["full_name", "skills"],
}


def parse_resume(raw_text: str) -> str:
    resp = client.chat.completions.create(
        model="gpt-5.1",
        response_format={"type": "json_schema", "json_schema": {"name": "resume", "schema": RESUME_SCHEMA}},
        messages=[{"role": "user", "content": raw_text}],
    )
    return resp.choices[0].message.content
