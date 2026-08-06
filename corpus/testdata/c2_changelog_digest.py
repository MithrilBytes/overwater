import os

from openai import OpenAI

client = OpenAI(api_key=os.environ["DEEPSEEK_API_KEY"], base_url="https://api.deepseek.com")


def digest_changelog(raw_commits: str) -> str:
    resp = client.chat.completions.create(
        model="deepseek-reasoner",
        messages=[
            {"role": "system", "content": "Write the weekly release digest from raw commit messages. Group by area and drop noise."},
            {"role": "user", "content": raw_commits},
        ],
    )
    return resp.choices[0].message.content
