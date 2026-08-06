from openai import OpenAI

client = OpenAI()


def moderate_comment(comment: str) -> bool:
    """Return True when the comment is safe to auto publish."""
    result = client.moderations.create(model="gpt-4o", input=comment)
    return not result.results[0].flagged
