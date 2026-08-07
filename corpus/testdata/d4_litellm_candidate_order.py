import litellm

PROMPT = "Given a job posting and candidate profiles, list the profile ids ordered by fit, best first. No commentary."


def order_candidates(posting: str, profiles: str) -> str:
    response = litellm.completion(
        model="anthropic/claude-haiku-4-5",
        messages=[
            {"role": "system", "content": PROMPT},
            {"role": "user", "content": posting + "\n" + profiles},
        ],
        temperature=0,
        max_tokens=100,
    )
    return response.choices[0].message.content
