import os

from mistralai import Mistral

client = Mistral(api_key=os.environ["CODESTRAL_API_KEY"])


def complete_span(prefix: str, suffix: str) -> str:
    response = client.fim.complete(
        model="codestral-2501",
        prompt=prefix,
        suffix=suffix,
        max_tokens=256,
        temperature=0.1,
    )
    return response.choices[0].message.content
