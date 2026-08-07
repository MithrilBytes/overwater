from openai import OpenAI

client = OpenAI()

PROMPT = "Translate each subtitle cue into the target language. Keep the cue numbers and timestamps byte for byte."


def translate_cues(srt: str, target: str) -> str:
    response = client.chat.completions.create(
        model="gpt-4.1",
        temperature=0,
        max_tokens=3000,
        messages=[
            {"role": "system", "content": PROMPT},
            {"role": "user", "content": "target: " + target + "\n" + srt},
        ],
    )
    return response.choices[0].message.content
