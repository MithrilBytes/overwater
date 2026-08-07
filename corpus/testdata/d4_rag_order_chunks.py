from openai import OpenAI

client = OpenAI()

PROMPT = (
    "You order retrieved chunks for a query. Return the chunk ids as a JSON array, "
    "most relevant first. Do not answer the question and do not add text."
)


def order_chunks(query: str, listing: str) -> str:
    response = client.chat.completions.create(
        model="gpt-4.1-nano",
        temperature=0,
        max_tokens=120,
        messages=[
            {"role": "system", "content": PROMPT},
            {"role": "user", "content": "query: " + query + "\n" + listing},
        ],
    )
    return response.choices[0].message.content
