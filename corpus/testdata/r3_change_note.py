"""From basicallysource/sorter-v2,
software/hive/backend/app/services/profile_ai.py, generate_change_note. The
model literal is the repository's own."""

from __future__ import annotations

from typing import Any

from app.services.openrouter import run_openrouter_chat


def generate_change_note(*, api_key: str, user_message: str, proposal: dict[str, Any]) -> str:
    """Use Haiku to generate a concise change note from the user request and AI proposal."""
    summary = proposal.get("summary", "")

    change_details: list[str] = []
    for p in proposal.get("proposals", []):
        action = p.get("action", "?")
        name = p.get("name", "rule")
        conditions = p.get("conditions") or []
        cond_strs = [f"{c.get('field')} {c.get('op')} {c.get('value')}" for c in conditions]
        detail = f"{action} \"{name}\""
        if cond_strs:
            detail += f" ({', '.join(cond_strs[:3])})"
        change_details.append(detail)
    changes_str = "\n".join(f"- {d}" for d in change_details) if change_details else "no changes"

    resp = run_openrouter_chat(
        api_key=api_key,
        model="anthropic/claude-haiku-4-5",
        messages=[
            {
                "role": "system",
                "content": (
                    "Generate a short change note (1 sentence, max 100 chars) for a LEGO sorting profile version. "
                    "Describe WHAT changed, not who requested it. "
                    "Examples: 'Add Technic sub-categories for gears, beams and axles', "
                    "'Remove duplicate plate rules', 'Split Bricks into basic and decorative'. "
                    "Return ONLY the change note, no quotes, no prefix. "
                    "Write in the same language as the user message."
                ),
            },
            {
                "role": "user",
                "content": f"User request: {user_message}\nAI summary: {summary}\nChanges:\n{changes_str}",
            },
        ],
        temperature=0.0,
        max_tokens=120,
    )
    return resp.content.strip().strip('"')
