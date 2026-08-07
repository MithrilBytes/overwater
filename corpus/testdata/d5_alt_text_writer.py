import base64

from openai import OpenAI

client = OpenAI()


def alt_text(image_bytes: bytes) -> str:
    encoded = base64.b64encode(image_bytes).decode()
    response = client.chat.completions.create(
        model="gpt-4o",
        max_tokens=120,
        messages=[
            {
                "role": "user",
                "content": [
                    {"type": "text", "text": "Describe what is in this photo in one sentence for a screen reader."},
                    {"type": "image_url", "image_url": {"url": "data:image/jpeg;base64," + encoded}},
                ],
            }
        ],
    )
    return response.choices[0].message.content
