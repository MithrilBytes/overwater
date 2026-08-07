import cohere

co = cohere.ClientV2()

TAXONOMY = "Choose the single topic that fits the message: shipping, returns, sizing, payment, other."


def topic_of(message: str) -> str:
    response = co.chat(
        model="command-r7b-12-2024",
        messages=[
            {"role": "system", "content": TAXONOMY},
            {"role": "user", "content": message},
        ],
        max_tokens=6,
        temperature=0,
    )
    return response.message.content[0].text.strip()
