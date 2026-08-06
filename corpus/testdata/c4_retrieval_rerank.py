import json

from openai import OpenAI

client = OpenAI()


def rerank_retrieved_chunks(query: str, chunks: list) -> list:
    listing = "\n".join(f"[{c['id']}] {c['text'][:200]}" for c in chunks)
    resp = client.chat.completions.create(
        model="gpt-5-nano",
        temperature=0,
        messages=[
            {
                "role": "system",
                "content": "You rerank retrieval results. Return a JSON array of chunk ids, most relevant to the query first.",
            },
            {"role": "user", "content": f"Query: {query}\n{listing}"},
        ],
    )
    return json.loads(resp.choices[0].message.content)
