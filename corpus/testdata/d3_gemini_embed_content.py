from google import genai

client = genai.Client()


def embed_passages(passages: list[str]):
    return client.models.embed_content(
        model="gemini-embedding-001",
        contents=passages,
        config={"task_type": "RETRIEVAL_DOCUMENT", "output_dimensionality": 768},
    )
