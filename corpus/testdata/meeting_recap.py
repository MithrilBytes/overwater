import anthropic

client = anthropic.Anthropic()


def summarize_meeting(transcript: str) -> str:
    message = client.messages.create(
        model="claude-sonnet-5",
        max_tokens=800,
        system="Turn raw meeting transcripts into five bullet points of decisions and owners.",
        messages=[{"role": "user", "content": transcript}],
    )
    return message.content[0].text
