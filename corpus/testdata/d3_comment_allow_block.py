from openai import OpenAI

client = OpenAI()

POLICY = (
    "You are the safety filter for user comments. Block harassment, threats, sexual content "
    "involving minors, and doxxing. Reply with ALLOW or BLOCK only."
)


def is_allowed(comment: str) -> bool:
    response = client.chat.completions.create(
        model="gpt-4.1-nano",
        temperature=0,
        max_tokens=3,
        messages=[
            {"role": "system", "content": POLICY},
            {"role": "user", "content": comment},
        ],
    )
    return response.choices[0].message.content.strip() == "ALLOW"
