from google import genai

client = genai.Client()

RECAP = (
    "Write the incident recap: what broke, the customer impact, the fix, and the follow up owed. "
    "Four short paragraphs, no bullet lists."
)


def recap(timeline: str) -> str:
    result = client.models.generate_content(
        model="gemini-2.5-flash",
        contents=timeline,
        config={"system_instruction": RECAP, "max_output_tokens": 1200},
    )
    return result.text
