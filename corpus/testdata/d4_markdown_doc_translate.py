import anthropic

client = anthropic.Anthropic()

PROMPT = (
    "Translate the documentation page into the target language. Preserve the markdown structure, "
    "code blocks, and link targets untouched. Do not summarize or reword."
)


def translate_page(page: str, target: str) -> str:
    message = client.messages.create(
        model="claude-sonnet-4-5",
        max_tokens=4000,
        temperature=0,
        system=PROMPT,
        messages=[{"role": "user", "content": "target: " + target + "\n\n" + page}],
    )
    return message.content[0].text
