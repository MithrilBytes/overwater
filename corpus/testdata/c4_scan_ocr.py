import anthropic

client = anthropic.Anthropic()


def ocr_scanned_page(page_b64: str) -> str:
    message = client.messages.create(
        model="claude-sonnet-5",
        max_tokens=4000,
        system=(
            "You perform OCR on scanned paperwork. Return the exact text in reading "
            "order. Mark unreadable spots with [illegible]."
        ),
        messages=[
            {
                "role": "user",
                "content": [
                    {
                        "type": "image",
                        "source": {"type": "base64", "media_type": "image/png", "data": page_b64},
                    },
                    {"type": "text", "text": "OCR this page."},
                ],
            }
        ],
    )
    return message.content[0].text
