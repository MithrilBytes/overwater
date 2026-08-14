"""Photo reading and note indexing. The two rules that key off shape.

Kept separate from triage.py because they are a different job: one reads
an image, one builds the index the triage step searches.
"""

from openai import OpenAI

openai_client = OpenAI()


def read_damage_photo(url: str):
    """Pull four fields off a photo of a bumper at full resolution.

    Trips image-detail-high. Reading a plate and a panel does not need
    the high detail tile budget, which multiplies the image tokens for a
    crop a person could read on a phone.
    """
    return openai_client.chat.completions.create(
        model="gpt-5.1",
        max_tokens=400,
        response_format={
            "type": "json_schema",
            "json_schema": {
                "name": "damage",
                "schema": {
                    "type": "object",
                    "properties": {
                        "plate": {"type": "string"},
                        "panel": {"type": "string"},
                        "severity": {"type": "string"},
                        "airbags": {"type": "boolean"},
                    },
                },
            },
        },
        messages=[
            {
                "role": "user",
                "content": [
                    {"type": "text", "text": "Read the plate and the damaged panel."},
                    {
                        "type": "image_url",
                        "image_url": {"url": url, "detail": "high"},
                    },
                ],
            }
        ],
    )


def index_notes(notes: list[str]):
    """Embed adjuster notes at whatever width the model ships.

    Trips uncapped-embedding-dimensions: text-embedding-3-large takes a
    dimensions argument and this asks for none, so every vector is
    stored and searched at 3072 wide for a corpus of short notes.
    """
    return openai_client.embeddings.create(
        model="text-embedding-3-large",
        input=notes,
    )
