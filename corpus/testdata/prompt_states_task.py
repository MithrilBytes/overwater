import anthropic

client = anthropic.Anthropic()


def handle_request(document: str) -> str:
    message = client.messages.create(
        model="claude-haiku-4-5",
        max_tokens=400,
        system="You summarize contract changes for the legal team in plain language.",
        messages=[{"role": "user", "content": document}],
    )
    return message.content[0].text
