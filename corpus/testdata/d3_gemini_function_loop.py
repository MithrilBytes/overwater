from google import genai
from google.genai import types

client = genai.Client()

TOOLS = types.Tool(function_declarations=[
    {"name": "get_inventory", "description": "Stock on hand for a sku."},
    {"name": "reorder", "description": "Place a restock order."},
])


def next_turn(contents: list) -> str:
    result = client.models.generate_content(
        model="gemini-3-pro",
        contents=contents,
        config=types.GenerateContentConfig(
            tools=[TOOLS],
            system_instruction="You are the inventory agent. Check stock, then reorder when a sku falls below its floor.",
            max_output_tokens=2000,
        ),
    )
    return result
