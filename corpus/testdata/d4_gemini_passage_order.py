from google import genai

client = genai.Client()

INSTRUCTION = "Reorder the passages by relevance to the question. Output the passage numbers, most relevant first."


def order_passages(question: str, passages: str) -> str:
    result = client.models.generate_content(
        model="gemini-2.0-flash",
        contents=question + "\n" + passages,
        config={"system_instruction": INSTRUCTION, "max_output_tokens": 80, "temperature": 0},
    )
    return result.text
