import litellm

POLICY = (
    "Decide whether this page is brand safe for advertising. Block pages about weapons, "
    "gambling, adult content, or graphic violence. Answer SAFE or UNSAFE."
)


def brand_safe(page_text: str) -> bool:
    response = litellm.completion(
        model="openai/gpt-4o-mini",
        messages=[
            {"role": "system", "content": POLICY},
            {"role": "user", "content": page_text},
        ],
        temperature=0,
        max_tokens=3,
    )
    return response.choices[0].message.content.strip() == "SAFE"
