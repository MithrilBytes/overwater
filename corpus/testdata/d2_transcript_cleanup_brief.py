import anthropic

client = anthropic.Anthropic()

# The recording pipeline hands us raw diarized text.
def brief_from_transcript(transcript: str) -> str:
    message = client.messages.create(
        model="claude-sonnet-4-5",
        max_tokens=600,
        system=(
            "Turn this call transcript into five bullets covering what was decided, what is owed, and by when. "
            "Drop small talk and repeated points. Do not reproduce the dialogue."
        ),
        messages=[{"role": "user", "content": transcript}],
    )
    return message.content[0].text
