from openai import AzureOpenAI

client = AzureOpenAI(api_version="2025-01-01-preview", azure_endpoint="https://acme.openai.azure.com")


def run_job(path: str) -> str:
    with open(path, "rb") as audio:
        result = client.audio.transcriptions.create(
            model="gpt-4o",
            file=audio,
            prompt="Keep filler words and false starts; this is a legal deposition.",
        )
    return result.text
