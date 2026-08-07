from openai import AzureOpenAI

client = AzureOpenAI(api_version="2025-01-01-preview", azure_endpoint="https://acme.openai.azure.com")

SCHEMA = {
    "type": "json_schema",
    "json_schema": {
        "name": "prescription",
        "schema": {
            "type": "object",
            "properties": {
                "drug_name": {"type": "string"},
                "dose": {"type": "string"},
                "frequency": {"type": "string"},
                "prescriber": {"type": "string"},
            },
        },
    },
}


def read_prescription(scan_text: str) -> str:
    response = client.chat.completions.create(
        model="gpt-4o",
        temperature=0,
        max_tokens=250,
        response_format=SCHEMA,
        messages=[
            {"role": "system", "content": "Copy the prescription fields as written. Do not normalize the dose."},
            {"role": "user", "content": scan_text},
        ],
    )
    return response.choices[0].message.content
