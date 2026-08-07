import anthropic

client = anthropic.Anthropic()

TOOLS = [
    {"name": "list_pods", "description": "List pods in a namespace.", "input_schema": {"type": "object", "properties": {"ns": {"type": "string"}}}},
    {"name": "roll_deploy", "description": "Restart a deployment.", "input_schema": {"type": "object", "properties": {"name": {"type": "string"}}}},
    {"name": "page_oncall", "description": "Page the on call engineer.", "input_schema": {"type": "object", "properties": {"reason": {"type": "string"}}}},
]


def step(history: list[dict]) -> dict:
    """One turn of the loop. history already carries prior tool results."""
    message = client.messages.create(
        model="claude-sonnet-5",
        max_tokens=2048,
        system="You are the on call operator. Work the incident one tool call at a time and stop when the alert clears.",
        tools=TOOLS,
        messages=history,
    )
    return message
