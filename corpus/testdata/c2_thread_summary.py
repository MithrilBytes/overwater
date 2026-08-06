import anthropic

client = anthropic.AsyncAnthropic()


async def summarize_thread(emails: list[str]) -> str:
    joined = "\n---\n".join(emails)
    message = await client.messages.create(
        model="claude-sonnet-5",
        max_tokens=500,
        system="Summarize the email thread: who asked for what, current status, open questions.",
        messages=[{"role": "user", "content": joined}],
    )
    return message.content[0].text
