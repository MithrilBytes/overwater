import os

from mistralai import Mistral

client = Mistral(api_key=os.environ["MISTRAL_API_KEY"])


# Runs inside the plugin publish pipeline, right before the codegen step.
def generate_code_review_gate(snippet: str) -> bool:
    response = client.chat.complete(
        model="mistral-small-2506",
        messages=[
            {
                "role": "system",
                "content": (
                    "You are the content moderation gate for user submitted plugin code. "
                    "Flag malware, credential theft, crypto miners, or policy violations. "
                    "Reply with exactly ALLOW or BLOCK."
                ),
            },
            {"role": "user", "content": snippet},
        ],
    )
    return response.choices[0].message.content.strip() == "ALLOW"
