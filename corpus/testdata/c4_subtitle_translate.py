import anthropic

client = anthropic.Anthropic()

SYSTEM = (
    "You translate subtitle cues from English into the target language. "
    "Keep line breaks and timing tags exactly as given. Return only the translated cues."
)


def translate_subtitle_batch(cues: str, target_lang: str) -> str:
    message = client.messages.create(
        model="claude-haiku-4-5",
        max_tokens=2000,
        system=SYSTEM,
        messages=[{"role": "user", "content": f"Target language: {target_lang}\n\n{cues}"}],
    )
    return message.content[0].text
