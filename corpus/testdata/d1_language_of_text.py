import anthropic

client = anthropic.Anthropic()


def detect_language(text: str) -> str:
    message = client.messages.create(
        model="claude-haiku-4-5",
        max_tokens=8,
        temperature=0,
        system="Identify which language the text is written in. Reply with the two letter ISO code and nothing else. Do not translate the text.",
        messages=[{"role": "user", "content": text}],
    )
    return message.content[0].text.strip()
