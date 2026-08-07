from openai import OpenAI

client = OpenAI()

WEATHER_TOOL = {
    "type": "function",
    "function": {
        "name": "get_forecast",
        "description": "Forecast for a city on a date.",
        "parameters": {"type": "object", "properties": {"city": {"type": "string"}, "date": {"type": "string"}}},
    },
}


def plan_turn(scratchpad: list[dict]) -> dict:
    """The loop keeps calling this with tool results appended until no tool call comes back."""
    return client.chat.completions.create(
        model="gpt-5-mini",
        messages=scratchpad,
        tools=[WEATHER_TOOL],
        max_tokens=1500,
    )
