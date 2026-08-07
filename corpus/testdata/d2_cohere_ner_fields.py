import cohere

co = cohere.ClientV2()

PROMPT = (
    "List every organization, person, and dollar amount named in the filing. "
    "Return JSON with the keys organizations, people, and amounts."
)


def named_entities(filing: str) -> str:
    response = co.chat(
        model="command-r-08-2024",
        messages=[
            {"role": "system", "content": PROMPT},
            {"role": "user", "content": filing},
        ],
        temperature=0,
        max_tokens=500,
    )
    return response.message.content[0].text
