import litellm

CHUNK_PROMPT = "Summarize this section of the report in three sentences. Keep every number that appears."


def summarize_chunk(chunk: str) -> str:
    response = litellm.completion(
        model="anthropic/claude-sonnet-4-5",
        messages=[
            {"role": "system", "content": CHUNK_PROMPT},
            {"role": "user", "content": chunk},
        ],
        max_tokens=500,
    )
    return response.choices[0].message.content
