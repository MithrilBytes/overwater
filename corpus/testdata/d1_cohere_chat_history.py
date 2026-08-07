import cohere

co = cohere.ClientV2()


def reply(user_message: str, chat_history: list[dict]) -> str:
    response = co.chat(
        model="command-a-03-2025",
        messages=chat_history + [{"role": "user", "content": user_message}],
        max_tokens=800,
        temperature=0.7,
    )
    return response.message.content[0].text
