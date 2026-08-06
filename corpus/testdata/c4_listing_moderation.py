from google import genai

client = genai.Client()

POLICY = "weapons, drugs, counterfeit goods, recalled items, or adult content"


def moderate_listing(listing_text: str) -> str:
    response = client.models.generate_content(
        model="gemini-2.5-flash",
        contents=(
            "You moderate marketplace listings. Flag any listing that offers "
            + POLICY
            + '. Return JSON {"allowed": bool, "reasons": []}.\n\nListing:\n'
            + listing_text
        ),
    )
    return response.text
