"""From basicallysource/sorter-v2, software/hive/backend/app/services/profile_ai.py
(and openrouter.py for the transport). The repository's DEFAULT_AI_MODEL,
z-ai/glm-5.2, is not in the catalog, so an id from its own curated roster is
substituted. The tool round loop is as written."""

from __future__ import annotations

from typing import Any

from app.services.openrouter import run_openrouter_chat

DEFAULT_AI_MODEL = "anthropic/claude-sonnet-5"
MAX_TOOL_ROUNDS = 5

_SEARCH_PARTS_TOOL = {
    "type": "function",
    "function": {
        "name": "search_parts",
        "description": (
            "Search the LEGO parts catalog by name, part number, or keyword. "
            "Returns matching parts with their category, BrickLink info, and year range. "
            "Only use this when you need to look up specific parts or verify part numbers."
        ),
        "parameters": {
            "type": "object",
            "properties": {
                "query": {"type": "string", "description": "Search term - part name, number, or keyword"},
                "limit": {"type": "integer", "description": "Max results to return (default 20, max 50)"},
            },
            "required": ["query"],
        },
    },
}

_SEARCH_SETS_TOOL = {
    "type": "function",
    "function": {
        "name": "search_sets",
        "description": "Search the official LEGO set catalog and return set_num, name, year, num_parts and img_url.",
        "parameters": {
            "type": "object",
            "properties": {"query": {"type": "string"}, "min_year": {"type": "integer"}},
            "required": ["query"],
        },
    },
}

_GET_SET_INVENTORY_TOOL = {
    "type": "function",
    "function": {
        "name": "get_set_inventory",
        "description": "Return the part inventory of one official LEGO set by set_num.",
        "parameters": {
            "type": "object",
            "properties": {"set_num": {"type": "string"}},
            "required": ["set_num"],
        },
    },
}

CATALOG_TOOLS = [_SEARCH_PARTS_TOOL, _SEARCH_SETS_TOOL, _GET_SET_INVENTORY_TOOL]

SYSTEM_PROMPT = """You maintain a LEGO sorting profile: a tree of rules that route parts into bins.

Guidelines:
- Distinguish carefully between official LEGO sets and custom sets.
- For edits and creates, always return COMPLETE conditions, not partial diffs.
- Before any create action, inspect Current rules carefully. If an equivalent rule already exists, do NOT create a duplicate.
- When the user wants to add a LEGO set, use search_sets to find the correct set_num, then use action "create_set".
- When the user asks which parts are inside an official LEGO set, first use search_sets to identify the exact set_num, then use get_set_inventory with that set_num.
- Treat search_sets results as the source of truth for set existence and metadata. Never list, recommend, compare, or create specific LEGO sets unless they appeared in search_sets output.
- If search_sets returns no results, say that clearly and do not fall back to your built-in knowledge."""


def propose_profile_changes(api_key: str, message: str, history: list[dict[str, Any]]):
    model = DEFAULT_AI_MODEL
    tools = CATALOG_TOOLS

    messages: list[dict[str, Any]] = [{"role": "system", "content": SYSTEM_PROMPT}]
    messages.extend(history)
    messages.append({"role": "user", "content": message})

    for round_index in range(MAX_TOOL_ROUNDS + 1):
        response = run_openrouter_chat(
            api_key=api_key,
            model=model,
            messages=messages,
            temperature=0.2,
            max_tokens=8192,
            tools=tools,
        )
        if not response.tool_calls:
            return response
        messages.append({"role": "assistant", "tool_calls": response.tool_calls})
        for call in response.tool_calls:
            messages.append({"role": "tool", "tool_call_id": call.id, "content": run_tool(call)})
    return response


def run_tool(call: Any) -> str:
    raise NotImplementedError
