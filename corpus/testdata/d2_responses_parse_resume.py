from openai import OpenAI
from pydantic import BaseModel

client = OpenAI()


class Resume(BaseModel):
    name: str
    email: str
    years_experience: int
    last_title: str


def parse_resume(text: str) -> Resume:
    response = client.responses.parse(
        model="gpt-4.1-mini",
        input=text,
        instructions="Pull the candidate fields out of the resume. Copy values exactly, leave a field empty when it is missing.",
        text_format=Resume,
        temperature=0,
        max_output_tokens=400,
    )
    return response.output_parsed
