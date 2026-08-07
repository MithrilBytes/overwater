from google import genai
from google.genai import types

client = genai.Client()


def write_out_audio(audio_bytes: bytes) -> str:
    result = client.models.generate_content(
        model="gemini-2.5-flash",
        contents=[
            types.Part.from_bytes(data=audio_bytes, mime_type="audio/wav"),
            "Write out everything the speakers say, word for word, with speaker labels and timestamps.",
        ],
        config={"max_output_tokens": 8000},
    )
    return result.text
