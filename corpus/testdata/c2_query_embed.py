import voyageai

vo = voyageai.Client()


# Query-side vectors for retrieval; must match the doc index dimensions.
def embed_search_query(query: str) -> list[float]:
    result = vo.embed(
        [query],
        model="voyage-3.5",
        input_type="query",
    )
    return result.embeddings[0]
