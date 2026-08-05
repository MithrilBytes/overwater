import numpy as np

from ingest import embed_chunks


def top_k(query: str, index: np.ndarray, k: int = 5) -> list[int]:
    vector = np.array(embed_chunks([query])[0])
    scores = index @ vector
    return list(np.argsort(scores)[::-1][:k])
