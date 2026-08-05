from openai import OpenAI

client = OpenAI()


def embed_chunks(chunks: list[str]) -> list[list[float]]:
    response = client.embeddings.create(
        model="text-embedding-3-large",
        input=chunks,
    )
    return [item.embedding for item in response.data]
