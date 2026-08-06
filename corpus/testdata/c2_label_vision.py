import base64

from openai import OpenAI

client = OpenAI()


def ocr_shipping_label(image_bytes: bytes) -> str:
    encoded = base64.b64encode(image_bytes).decode()
    resp = client.chat.completions.create(
        model="gpt-4o",
        messages=[
            {
                "role": "user",
                "content": [
                    {"type": "text", "text": "Read the carrier and tracking number off this shipping label photo."},
                    {"type": "image_url", "image_url": {"url": f"data:image/jpeg;base64,{encoded}"}},
                ],
            }
        ],
    )
    return resp.choices[0].message.content
