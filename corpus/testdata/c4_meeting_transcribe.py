from openai import OpenAI

client = OpenAI()


def transcribe_meeting(path: str) -> str:
    with open(path, "rb") as audio_file:
        result = client.audio.transcriptions.create(
            model="gpt-4o",
            file=audio_file,
            response_format="text",
            language="en",
        )
    return result
