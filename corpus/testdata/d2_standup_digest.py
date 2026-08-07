from openai import OpenAI

client = OpenAI()

DIGEST = (
    "Condense the standup notes into a short paragraph for the manager. "
    "Lead with anything blocked, then progress. Do not list every person."
)


def digest(notes: str) -> str:
    response = client.chat.completions.create(
        model="gpt-4.1-mini",
        max_tokens=400,
        messages=[
            {"role": "system", "content": DIGEST},
            {"role": "user", "content": notes},
        ],
    )
    return response.choices[0].message.content
