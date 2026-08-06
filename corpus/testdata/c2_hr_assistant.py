import anthropic

client = anthropic.AsyncAnthropic()

BENEFITS_SYSTEM = "You are the benefits assistant for new hires. Answer from the handbook and keep the conversation reassuring."


async def chat_about_benefits(history: list[dict]) -> str:
    message = await client.messages.create(
        model="claude-opus-5",
        max_tokens=600,
        system=BENEFITS_SYSTEM,
        messages=history,
    )
    return message.content[0].text
