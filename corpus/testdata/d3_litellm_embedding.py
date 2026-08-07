import litellm


def vectorize(texts: list[str]):
    return litellm.embedding(model="text-embedding-3-small", input=texts)
