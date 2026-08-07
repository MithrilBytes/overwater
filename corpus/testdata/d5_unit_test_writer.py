from openai import OpenAI

client = OpenAI()

PROMPT = "Write pytest tests for the function below. Output only the test file contents, no explanation."


def write_tests(source: str) -> str:
    response = client.chat.completions.create(
        model="gpt-5.1",
        max_tokens=2000,
        temperature=0.2,
        messages=[
            {"role": "system", "content": PROMPT},
            {"role": "user", "content": source},
        ],
    )
    return response.choices[0].message.content
