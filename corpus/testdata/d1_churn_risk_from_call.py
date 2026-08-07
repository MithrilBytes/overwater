import litellm

RUBRIC = (
    "Given a sales call transcript, rate the account's churn risk. "
    "Answer with one of: high, medium, low. Nothing else."
)


def churn_risk(transcript: str) -> str:
    response = litellm.completion(
        model="anthropic/claude-haiku-4-5",
        messages=[
            {"role": "system", "content": RUBRIC},
            {"role": "user", "content": transcript},
        ],
        temperature=0,
        max_tokens=5,
    )
    return response.choices[0].message.content.strip()
