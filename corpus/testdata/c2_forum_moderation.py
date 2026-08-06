import anthropic

client = anthropic.Anthropic()

POLICY = "Decide whether the comment breaks a forum rule: harassment, doxxing, spam, or none. Answer with the single rule name."


def moderate_comment(comment: str) -> str:
    message = client.messages.create(
        model="claude-haiku-4-5",
        max_tokens=10,
        system=POLICY,
        messages=[{"role": "user", "content": comment}],
    )
    return message.content[0].text.strip()
