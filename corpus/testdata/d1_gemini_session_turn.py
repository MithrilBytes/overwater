from google import genai

client = genai.Client()


def send_turn(session_history: list[dict], message: str) -> str:
    chat = client.chats.create(
        model="gemini-2.5-flash",
        history=session_history,
        config={
            "system_instruction": "You are the museum guide assistant. Answer visitors in two friendly sentences.",
            "max_output_tokens": 600,
            "temperature": 0.7,
        },
    )
    return chat.send_message(message).text
