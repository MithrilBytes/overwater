from openai import OpenAI

client = OpenAI()


def embed_kb_chunks(chunks: list[str]) -> list[list[float]]:
    resp = client.embeddings.create(
        model="text-embedding-3-large",
        input=chunks,
        dimensions=1024,
    )
    return [item.embedding for item in resp.data]
