import anthropic

client = anthropic.Anthropic()


def run_turn(history: list[dict], mcp_tools: list[dict]):
    """Tools come from the connected MCP servers; the loop runs until the model stops asking."""
    return client.messages.create(
        model="claude-opus-4-5",
        max_tokens=4096,
        system="You are a coding agent with filesystem and git tools. Plan, act, and verify before you stop.",
        tools=mcp_tools,
        messages=history,
    )
