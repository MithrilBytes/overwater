from openai import OpenAI

client = OpenAI()

SYSTEM_PROMPT = "You are the Overwater support assistant. Keep the conversation friendly and resolve the issue in as few turns as possible."


def stream_chat_reply(history: list[dict]):
    stream = client.chat.completions.create(
        model="gpt-4o",
        stream=True,
        messages=[{"role": "system", "content": SYSTEM_PROMPT}, *history],
    )
    for chunk in stream:
        delta = chunk.choices[0].delta.content
        if delta:
            yield delta
