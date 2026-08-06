from google import genai
from google.genai import types

client = genai.Client()


# Nightly digest job: turns yesterday's support calls into text for the CRM.
def summarize_customer_call(audio_bytes: bytes) -> str:
    response = client.models.generate_content(
        model="gemini-2.5-pro",
        contents=[
            types.Part.from_bytes(data=audio_bytes, mime_type="audio/wav"),
            (
                "Write out every word spoken in this recording exactly as heard, in "
                "order, with [mm:ss] timestamps. Do not shorten, skip, or reword anything."
            ),
        ],
    )
    return response.text
