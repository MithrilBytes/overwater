import litellm

PROMPT = "Translate each numbered line into the target language. Return the same numbering, one line per entry."


def translate_lines(lines: str, target: str) -> str:
    response = litellm.completion(
        model="mistral/mistral-medium-3",
        messages=[
            {"role": "system", "content": PROMPT},
            {"role": "user", "content": target + "\n" + lines},
        ],
        temperature=0,
        max_tokens=2000,
    )
    return response.choices[0].message.content
