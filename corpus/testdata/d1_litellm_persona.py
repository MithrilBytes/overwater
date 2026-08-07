import litellm


def talk(turns: list[dict]) -> str:
    response = litellm.completion(
        model="mistral/mistral-medium-3",
        messages=[
            {
                "role": "system",
                "content": "You play a gruff but kind harbor master. Stay in character and keep replies to a paragraph.",
            }
        ]
        + turns,
        max_tokens=600,
        temperature=0.8,
    )
    return response.choices[0].message.content
