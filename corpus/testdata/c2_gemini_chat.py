from google import genai

client = genai.Client()


def chat_reply(history: list[dict]) -> str:
    resp = client.models.generate_content(
        model="gemini-2.5-flash",
        contents=history,
        config={"system_instruction": "You are the in-app assistant. Keep the conversation short and concrete."},
    )
    return resp.text
