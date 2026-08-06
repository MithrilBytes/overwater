import os

from mistralai import Mistral

client = Mistral(api_key=os.environ["MISTRAL_API_KEY"])


def classify_review_sentiment(review: str) -> str:
    resp = client.chat.complete(
        model="mistral-small-2506",
        temperature=0,
        messages=[
            {"role": "system", "content": "Label the product review as positive, negative, or mixed. Reply with the label only."},
            {"role": "user", "content": review},
        ],
    )
    return resp.choices[0].message.content.strip().lower()
