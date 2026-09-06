"""From christianlouis/DocuElevate, app/utils/i18n.py: the AI fallback that
translates a UI string the JSON bundles do not carry. The 77 row language
table is cut to a few rows, enough for the fallback to name its target; the
model is read off settings with the getattr default the repository uses."""

from __future__ import annotations

import logging

logger = logging.getLogger(__name__)

# ---------------------------------------------------------------------------
# Supported languages (ordered by priority)
# ---------------------------------------------------------------------------

SUPPORTED_LANGUAGES: list[dict[str, str]] = [
    # --- Tier 1: Primary European languages ---
    {"code": "en", "name": "English", "native": "English", "flag": "gb"},
    {"code": "de", "name": "German", "native": "Deutsch", "flag": "de"},
    {"code": "fr", "name": "French", "native": "Français", "flag": "fr"},
    {"code": "es", "name": "Spanish", "native": "Español", "flag": "es"},
    # --- Tier 5: Asian languages ---
    {"code": "ja", "name": "Japanese", "native": "日本語", "flag": "jp"},
    {"code": "ko", "name": "Korean", "native": "한국어", "flag": "kr"},
]

SUPPORTED_LANGUAGE_CODES: set[str] = {lang["code"] for lang in SUPPORTED_LANGUAGES}
DEFAULT_LANGUAGE = "en"

_ai_translation_cache: dict[tuple[str, str], str] = {}


def translate_with_ai_fallback(text: str, target_locale: str) -> str:
    """Translate *text* using the configured AI provider as a fallback.

    Returns the original *text* unchanged when:
    * The target locale is English (source language)
    * The AI provider is unavailable or returns an error
    * The translation has already been cached

    Results are cached in-memory for the lifetime of the process.
    """
    if target_locale == DEFAULT_LANGUAGE or target_locale not in SUPPORTED_LANGUAGE_CODES:
        return text

    cache_key = (text, target_locale)
    if cache_key in _ai_translation_cache:
        return _ai_translation_cache[cache_key]

    target_name = next(
        (lang["name"] for lang in SUPPORTED_LANGUAGES if lang["code"] == target_locale),
        target_locale,
    )

    try:
        from litellm import completion  # type: ignore[import-untyped]

        from app.config import settings

        model = getattr(settings, "ai_model", None) or getattr(settings, "openai_model", "gpt-4o-mini")
        response = completion(
            model=model,
            messages=[
                {
                    "role": "system",
                    "content": (
                        f"You are a professional translator. Translate the following UI text "
                        f"from English to {target_name}. Return ONLY the translated text, "
                        f"nothing else. Keep any HTML tags, placeholders like {{name}}, "
                        f"and special characters intact."
                    ),
                },
                {"role": "user", "content": text},
            ],
            max_tokens=256,
            temperature=0.1,
        )
        translated = response.choices[0].message.content.strip()
        _ai_translation_cache[cache_key] = translated
        return translated
    except Exception:
        logger.debug("AI fallback translation failed for '%s' -> %s", text[:50], target_locale)
        return text
