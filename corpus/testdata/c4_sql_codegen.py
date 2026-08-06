import os

from openai import OpenAI

client = OpenAI(base_url="https://api.deepseek.com", api_key=os.environ["DEEPSEEK_API_KEY"])


def sql_codegen(question: str, ddl: str) -> str:
    """Turn an analyst question into a runnable BigQuery query."""
    resp = client.chat.completions.create(
        model="deepseek-chat",
        temperature=0,
        messages=[
            {
                "role": "system",
                "content": "Write one SQL query answering the question against the given schema. Output only SQL, no prose.",
            },
            {"role": "user", "content": f"Schema:\n{ddl}\n\nQuestion: {question}"},
        ],
    )
    return resp.choices[0].message.content
