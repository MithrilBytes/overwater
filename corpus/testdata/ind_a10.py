from openai import OpenAI
c = OpenAI()
def to_text(path):
    with open(path, "rb") as f:
        return c.audio.transcriptions.create(model="gpt-4o", file=f)
