import cohere

co = cohere.ClientV2()


def index_vectors(texts: list[str]):
    return co.embed(
        model="embed-v4.0",
        texts=texts,
        input_type="search_document",
        embedding_types=["float"],
    )
