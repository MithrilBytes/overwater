from openai import OpenAI

client = OpenAI()

PROMPT = (
    "Read the support thread, then decide what happens to it. "
    "Answer with exactly one word: ESCALATE, WAIT, or CLOSE. No explanation, no summary."
)


def decide(thread: str) -> str:
    response = client.chat.completions.create(
        model="gpt-4o-mini",
        temperature=0,
        max_tokens=6,
        messages=[
            {"role": "system", "content": PROMPT},
            {"role": "user", "content": thread},
        ],
    )
    return response.choices[0].message.content.strip()
