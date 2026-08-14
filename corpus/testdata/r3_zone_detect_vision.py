"""From basicallysource/sorter-v2,
software/sorter/backend/vision/gemini_sam_detector.py. The repository's
DEFAULT_OPENROUTER_MODEL, google/gemini-3-flash-preview, is not in the
catalog, so a catalog id is substituted; the prompt is trimmed to the
classification-chamber zone."""

from __future__ import annotations

import os
from typing import Any

DEFAULT_OPENROUTER_MODEL = "google/gemini-2.0-flash"
OPENROUTER_BASE_URL = "https://openrouter.ai/api/v1"
OPENROUTER_API_TIMEOUT_S = 60.0

SYSTEM_PROMPT = (
    "You are a precise object detector for a LEGO sorting machine. The machine "
    "is expected to process LEGO pieces but it also needs to notice anything "
    "else that ended up in the workflow - screws, coins, pebbles, plastic "
    "fragments, tape, hair, wrappers, any foreign object. Detect LEGO pieces "
    "AND non-LEGO items with equal attention. "
    "Respond with valid JSON only - no markdown, no prose, no explanations."
)

CHAMBER_PROMPT = (
    "You are detecting loose physical objects in the classification chamber from a "
    "top-down {width}x{height} camera image.\n\n"
    "Return one tight bounding box for each loose physical item on the tray. Detect "
    "LEGO/compatible plastic parts and foreign objects such as screws, coins, stones, "
    "tape, hair, wrappers, fragments, tools, or unknown debris.\n\n"
    "Ignore the tray surface, the LED ring and its bright halo, specular reflections on "
    "the tray, and shadows cast by the piece. Do NOT shrink a piece's bounding box to "
    "exclude glare or highlights on the piece itself - glare is part of the piece.\n\n"
    "Output JSON only:\n"
    '{{"detections": [{{"kind": "lego|foreign", "description": "<short label>", '
    '"bbox": [y_min, x_min, y_max, x_max], "confidence": 0.0}}]}}\n'
    "bbox is normalized 0-1000, ordered [y_min, x_min, y_max, x_max].\n"
    'If no valid objects are visible, return {{"detections":[]}}'
)


def _call_openrouter(prompt: str, image_b64: str, *, model: str) -> dict[str, Any]:
    api_key = os.getenv("OPENROUTER_API_KEY")
    if not api_key:
        raise RuntimeError("OPENROUTER_API_KEY is not set.")
    from openai import OpenAI

    client = OpenAI(base_url=OPENROUTER_BASE_URL, api_key=api_key)
    response = client.chat.completions.create(
        model=model or DEFAULT_OPENROUTER_MODEL,
        messages=[
            {"role": "system", "content": SYSTEM_PROMPT},
            {
                "role": "user",
                "content": [
                    {"type": "text", "text": prompt},
                    {"type": "image_url", "image_url": {"url": f"data:image/jpeg;base64,{image_b64}"}},
                ],
            },
        ],
        temperature=0.1,
        # Dense C-channel frames can contain many parts plus short descriptions.
        # Keep enough headroom so the teacher returns valid JSON instead of a
        # truncated object that later becomes a false "no detections" sample.
        max_tokens=2048,
        timeout=OPENROUTER_API_TIMEOUT_S,
    )
    return _extract_json(response.choices[0].message.content.strip())


def _extract_json(text: str) -> dict[str, Any]:
    raise NotImplementedError
