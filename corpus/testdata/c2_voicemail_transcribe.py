from google import genai

client = genai.Client()


def transcribe_voicemail(audio_path: str) -> str:
    clip = client.files.upload(file=audio_path)
    resp = client.models.generate_content(
        model="gemini-2.5-flash",
        contents=["Transcribe the recording word for word. Mark unclear spans with [inaudible].", clip],
    )
    return resp.text
