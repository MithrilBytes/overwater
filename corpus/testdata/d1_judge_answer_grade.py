from openai import OpenAI

client = OpenAI()

GRADER = (
    "You grade a model answer against the reference answer. "
    "Reply with one word: correct, partial, or incorrect."
)


def grade(reference: str, candidate: str) -> str:
    response = client.chat.completions.create(
        model="gpt-4.1",
        temperature=0,
        max_tokens=4,
        messages=[
            {"role": "system", "content": GRADER},
            {"role": "user", "content": "reference: " + reference + "\ncandidate: " + candidate},
        ],
    )
    return response.choices[0].message.content.strip()
