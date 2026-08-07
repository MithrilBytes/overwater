import os

from mistralai import Mistral

client = Mistral(api_key=os.environ["MISTRAL_API_KEY"])

PROMPT = (
    "Return JSON with the keys carrier, tracking_number, ship_date, and destination_zip, "
    "copied from the shipping email. Use null when a key is not present."
)


def read_shipment(email: str) -> str:
    response = client.chat.complete(
        model="mistral-small-2506",
        temperature=0,
        max_tokens=300,
        response_format={"type": "json_object"},
        messages=[
            {"role": "system", "content": PROMPT},
            {"role": "user", "content": email},
        ],
    )
    return response.choices[0].message.content
