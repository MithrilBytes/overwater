from google import genai
from google.genai import types

client = genai.Client()


def caption(image_bytes: bytes) -> str:
    result = client.models.generate_content(
        model="gemini-2.5-flash",
        contents=[
            types.Part.from_bytes(data=image_bytes, mime_type="image/png"),
            "Caption this image for the photo library index. Mention the setting and any visible text.",
        ],
        config={"max_output_tokens": 200},
    )
    return result.text
