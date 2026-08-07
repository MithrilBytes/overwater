from google import genai

client = genai.Client()

POLICY = "Screen the upload caption against community guidelines. Answer approve or remove."


def screen_caption(caption: str) -> str:
    result = client.models.generate_content(
        model="gemini-2.0-flash-lite",
        contents=caption,
        config={"system_instruction": POLICY, "max_output_tokens": 4, "temperature": 0},
    )
    return result.text.strip()
