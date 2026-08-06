import json

from openai import OpenAI

client = OpenAI()


def rerank_passages(query: str, passages: list[str]) -> list[int]:
    numbered = "\n".join(f"[{i}] {p}" for i, p in enumerate(passages))
    resp = client.chat.completions.create(
        model="gpt-5-mini",
        temperature=0,
        messages=[
            {"role": "system", "content": "Order the numbered passages from most to least relevant to the query. Return a JSON array of indexes only."},
            {"role": "user", "content": f"Query: {query}\n\n{numbered}"},
        ],
    )
    return json.loads(resp.choices[0].message.content)
