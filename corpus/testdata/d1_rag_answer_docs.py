from openai import OpenAI

client = OpenAI()

ANSWER_PROMPT = (
    "Answer the customer's question using only the documentation passages provided. "
    "Write two or three sentences in a friendly voice. If the passages do not cover it, "
    "say so and offer to open a ticket."
)


def answer_from_docs(question: str, passages: list[str]) -> str:
    response = client.chat.completions.create(
        model="gpt-4.1-mini",
        max_tokens=500,
        messages=[
            {"role": "system", "content": ANSWER_PROMPT},
            {"role": "user", "content": "\n\n".join(passages) + "\n\nQuestion: " + question},
        ],
    )
    return response.choices[0].message.content
