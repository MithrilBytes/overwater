import anthropic

client = anthropic.Anthropic()

DESKTOP_TOOLS = [
    {"type": "computer_20250124", "name": "computer", "display_width_px": 1280, "display_height_px": 800},
    {"type": "bash_20250124", "name": "bash"},
]


def next_action(history: list[dict]):
    """Drives the VM: each turn returns the next click or keystroke to run."""
    return client.messages.create(
        model="claude-opus-4-6",
        max_tokens=4096,
        system="You drive the test VM. Take one action at a time and check the screen after each one.",
        tools=DESKTOP_TOOLS,
        messages=history,
    )
