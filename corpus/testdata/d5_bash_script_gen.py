import litellm

PROMPT = "Write a bash script that does what the user describes. Use set -euo pipefail. Output the script only."


def write_script(ask: str) -> str:
    response = litellm.completion(
        model="anthropic/claude-sonnet-4-5",
        messages=[
            {"role": "system", "content": PROMPT},
            {"role": "user", "content": ask},
        ],
        max_tokens=1200,
        temperature=0.1,
    )
    return response.choices[0].message.content
