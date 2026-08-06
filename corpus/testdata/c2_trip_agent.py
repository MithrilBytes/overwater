import anthropic

client = anthropic.Anthropic()

FLIGHT_TOOLS = [
    {
        "name": "search_flights",
        "description": "Search fares for a route and date.",
        "input_schema": {
            "type": "object",
            "properties": {"origin": {"type": "string"}, "dest": {"type": "string"}, "date": {"type": "string"}},
            "required": ["origin", "dest"],
        },
    },
    {
        "name": "hold_seat",
        "description": "Place a 24 hour hold on a fare.",
        "input_schema": {"type": "object", "properties": {"fare_id": {"type": "string"}}, "required": ["fare_id"]},
    },
]


def plan_trip_step(turns: list[dict]):
    return client.messages.create(
        model="claude-sonnet-5",
        max_tokens=1024,
        tools=FLIGHT_TOOLS,
        messages=turns,
    )
