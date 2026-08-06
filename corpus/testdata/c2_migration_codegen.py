import os

import openai

openai.api_key = os.environ["OPENAI_API_KEY"]


def generate_code_for_migration(table_ddl: str) -> str:
    completion = openai.Completion.create(
        model="text-davinci-003",
        prompt="Write an Alembic migration for this schema change. Return only Python code.\n\n" + table_ddl,
        max_tokens=400,
        temperature=0,
    )
    return completion.choices[0].text
