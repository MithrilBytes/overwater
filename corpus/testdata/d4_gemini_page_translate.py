from google import genai

client = genai.Client()

INSTRUCTION = "Translate the page into the target language. Keep the heading levels and bullet markers exactly as given."


def translate(page: str, target: str) -> str:
    result = client.models.generate_content(
        model="gemini-2.5-flash",
        contents="target: " + target + "\n" + page,
        config={"system_instruction": INSTRUCTION, "max_output_tokens": 3000, "temperature": 0},
    )
    return result.text
