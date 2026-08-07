import anthropic

client = anthropic.Anthropic()


def read_receipt_photo(photo_b64: str) -> str:
    message = client.messages.create(
        model="claude-haiku-4-5",
        max_tokens=400,
        messages=[
            {
                "role": "user",
                "content": [
                    {"type": "image", "source": {"type": "base64", "media_type": "image/jpeg", "data": photo_b64}},
                    {"type": "text", "text": "Read the merchant, date, and total off this receipt photo."},
                ],
            }
        ],
    )
    return message.content[0].text
