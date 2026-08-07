import os

from mistralai import Mistral

client = Mistral(api_key=os.environ["MISTRAL_API_KEY"])


def embed_batch(batch: list[str]):
    return client.embeddings.create(model="mistral-embed", inputs=batch)
