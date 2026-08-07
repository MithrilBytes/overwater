import anthropic

client = anthropic.Anthropic()


def continue_conversation(turns: list[dict]) -> str:
    message = client.messages.create(
        model="claude-sonnet-5",
        max_tokens=1024,
        system="You are a patient study companion. Talk the student through the problem instead of handing over the answer.",
        messages=turns,
    )
    return message.content[0].text
