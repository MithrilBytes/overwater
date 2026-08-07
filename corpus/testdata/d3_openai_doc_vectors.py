from openai import OpenAI

client = OpenAI()


def embed_documents(chunks: list[str]) -> list[list[float]]:
    response = client.embeddings.create(
        model="text-embedding-3-large",
        input=chunks,
        dimensions=1536,
    )
    return [item.embedding for item in response.data]
