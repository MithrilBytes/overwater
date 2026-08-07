from openai import OpenAI

client = OpenAI()


def transcribe_call(path: str) -> str:
    with open(path, "rb") as audio:
        result = client.audio.transcriptions.create(
            model="gpt-4o-mini",
            file=audio,
            language="en",
            response_format="verbose_json",
        )
    return result.text
