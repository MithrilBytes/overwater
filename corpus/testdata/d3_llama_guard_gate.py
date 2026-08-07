from groq import Groq

client = Groq()


def unsafe(user_text: str) -> bool:
    """Guard pass in front of the main model."""
    response = client.chat.completions.create(
        model="llama-3.1-8b-instant",
        temperature=0,
        max_tokens=4,
        messages=[
            {"role": "system", "content": "Decide whether the message violates the usage policy. Answer safe or unsafe."},
            {"role": "user", "content": user_text},
        ],
    )
    return response.choices[0].message.content.strip() == "unsafe"
