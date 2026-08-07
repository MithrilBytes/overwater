from openai import OpenAI

client = OpenAI()

SYSTEM = (
    "You are the Acme support assistant. Answer the customer in a warm, plain tone, "
    "ask a follow up question when the report is vague, and never invent an account detail."
)


def reply_to_thread(history: list[dict]) -> str:
    stream = client.chat.completions.create(
        model="gpt-5.1",
        messages=[{"role": "system", "content": SYSTEM}] + history,
        stream=True,
        max_tokens=800,
    )
    return "".join(chunk.choices[0].delta.content or "" for chunk in stream)
