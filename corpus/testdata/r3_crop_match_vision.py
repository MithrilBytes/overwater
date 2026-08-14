"""From basicallysource/sorter-v2,
software/hive/backend/app/services/piece_crop_ai_matcher.py. The
repository's DEFAULT_MATCH_MODEL, google/gemini-3.5-flash, is not in the
catalog, so a catalog id is substituted; the call is otherwise as written."""

from __future__ import annotations

import base64
import io
import json
import urllib.request
from typing import Any, Optional

from PIL import Image

DEFAULT_MATCH_MODEL = "google/gemini-2.5-flash"
OPENROUTER_TIMEOUT_S = 180.0
OPENROUTER_BASE_URL = "https://openrouter.ai/api/v1"

GRID_PROMPT = (
    "Image 1 shows one LEGO piece photographed in the classification chamber of a "
    "sorting machine (possibly several views of it side by side).\n"
    "Image 2 is a numbered grid of small crops from cameras EARLIER in the machine. "
    "Some crops may show the same physical piece moments before it reached the "
    "chamber; most show other pieces.\n\n"
    "Find every numbered cell that shows the SAME physical piece as Image 1 - same "
    "shape/mold AND same color. Judge by appearance only.\n"
    "Only count a cell if the piece is COMPLETELY inside the crop (not cut off at an "
    "edge). If the same piece appears but is partially cut off, do not count that cell.\n"
    "If no cell qualifies, return an empty list.\n\n"
    'Respond with valid JSON only, no markdown: {"matches": [cell numbers], '
    '"reasoning": "one short sentence"}'
)


def _image_part(image: Image.Image) -> dict[str, Any]:
    buf = io.BytesIO()
    image.save(buf, format="JPEG", quality=88)
    b64 = base64.b64encode(buf.getvalue()).decode("ascii")
    return {"type": "image_url", "image_url": {"url": f"data:image/jpeg;base64,{b64}"}}


def _call_openrouter(model: str, content: list[dict[str, Any]], api_key: str) -> dict[str, Any]:
    payload = {
        "model": model,
        "messages": [{"role": "user", "content": content}],
        "temperature": 0.1,
        "max_tokens": 2048,
        "usage": {"include": True},
    }
    req = urllib.request.Request(
        f"{OPENROUTER_BASE_URL}/chat/completions",
        data=json.dumps(payload).encode(),
        headers={"Authorization": f"Bearer {api_key}", "Content-Type": "application/json"},
        method="POST",
    )
    with urllib.request.urlopen(req, timeout=OPENROUTER_TIMEOUT_S) as resp:
        data = json.loads(resp.read().decode())
    return json.loads(data["choices"][0]["message"]["content"] or "{}")


def match_crops(anchor: Image.Image, grid: Image.Image, api_key: str, model: Optional[str] = None):
    content = [
        {"type": "text", "text": GRID_PROMPT},
        _image_part(anchor),
        _image_part(grid),
    ]
    parsed = _call_openrouter(model or DEFAULT_MATCH_MODEL, content, api_key)
    return [int(p) for p in (parsed.get("matches") or [])]
