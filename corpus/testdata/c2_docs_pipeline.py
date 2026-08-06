from openai import OpenAI

client = OpenAI()

# Translation pass for the localization pipeline; output lands in the es-MX docs build.
PIPELINE_PROMPT = "Boil the engineering spec down to five plain-English bullets for the weekly team digest."


def process_spec(spec_markdown: str) -> str:
    resp = client.chat.completions.create(
        model="o3-mini",
        messages=[
            {"role": "system", "content": PIPELINE_PROMPT},
            {"role": "user", "content": spec_markdown},
        ],
    )
    translated = resp.choices[0].message.content
    return translated
