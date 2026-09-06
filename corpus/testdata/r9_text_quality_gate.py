"""From christianlouis/DocuElevate, app/utils/text_quality.py: the AI pass of
check_text_quality that decides whether the text embedded in a PDF is usable
or the page goes back through OCR. The producer sniffing that classifies the
text source, the digital and empty text short circuits, and the head to head
OCR comparison that follows are dropped. The provider wrapper is reached
through app.utils.ai_provider, as in the repository."""

import json
import logging
import re
from dataclasses import dataclass, field
from enum import Enum
from typing import Optional

from app.config import settings
from app.utils.ai_provider import get_ai_provider

logger = logging.getLogger(__name__)

# Maximum characters of text forwarded to the AI for quality assessment.
_TEXT_SAMPLE_MAX_CHARS = 3000


class TextSource(str, Enum):
    """Indicates the origin of text embedded in a PDF."""

    DIGITAL = "digital"  # Created by a digital authoring tool (Word, LibreOffice, LaTeX...)
    OCR_PREVIOUS = "ocr"  # Previously run through an OCR engine
    UNKNOWN = "unknown"  # Source cannot be determined


@dataclass
class TextQualityResult:
    """Result of an embedded-text quality assessment."""

    is_good_quality: bool
    quality_score: int  # 0-100; 0 = completely garbled, 100 = perfect
    text_source: TextSource
    feedback: str
    issues: list[str] = field(default_factory=list)
    ai_response_raw: Optional[str] = None


def check_text_quality(text: str, text_source: TextSource) -> TextQualityResult:
    """Assess the quality of embedded PDF text using an AI model.

    Text from a previous OCR pass, or of unknown origin, is assessed for:

    - Excessive typos and OCR character-substitution artefacts.
    - Lack of semantic coherence.
    - Garbage characters or symbol soup.

    The acceptance criteria are controlled by two settings:

    - ``settings.text_quality_threshold``: minimum score (default 85) for
      auto-acceptance.
    - ``settings.text_quality_significant_issues``: list of issue labels that
      force re-OCR even when the score meets the threshold (e.g.
      ``excessive_typos``, ``garbage_characters``, ``incoherent_text``,
      ``fragmented_sentences``).
    """
    stripped = text.strip()

    # 3. Retrieve configurable thresholds.
    threshold = getattr(settings, "text_quality_threshold", 85)
    significant_issues: list[str] = list(
        getattr(
            settings,
            "text_quality_significant_issues",
            ["excessive_typos", "garbage_characters", "incoherent_text", "fragmented_sentences"],
        )
    )

    sample = stripped[:_TEXT_SAMPLE_MAX_CHARS]
    logger.info(
        f"[text_quality] Assessing text quality "
        f"(source={text_source.value}, sample_chars={len(sample)}, total_chars={len(stripped)}, "
        f"threshold={threshold})"
    )

    prompt = (
        "You are a document quality assessor. Your task is to evaluate whether the "
        "text extracted from a PDF is high-quality and semantically meaningful, or "
        "whether it looks like garbled OCR output with typos, garbage characters, or "
        "nonsensical fragments.\n\n"
        "Evaluate the following text and return a JSON object with exactly these fields:\n"
        '  "quality_score": integer 0-100 (0=completely garbled, 100=perfect text)\n'
        f'  "is_good_quality": boolean (true if quality_score >= {threshold} AND no significant issues)\n'
        '  "feedback": one-sentence summary of your assessment\n'
        '  "issues": list of issues found (e.g. ["excessive_typos", "garbage_characters", '
        '"incoherent_text", "fragmented_sentences"]); empty list if none\n\n'
        f"Criteria for POOR quality (score < {threshold}):\n"
        "- Excessive typos, misspellings, or letter substitutions typical of OCR errors\n"
        "- Garbage characters (%, @, #, symbols mixed randomly into words)\n"
        "- Incoherent or nonsensical sentences that carry no meaning\n"
        "- Sequences of random characters or numbers without context\n"
        "- Heavy fragmentation (isolated letters or words without sentence structure)\n\n"
        f"Criteria for GOOD quality (score >= {threshold}):\n"
        "- Mostly readable text with at most very minor imperfections\n"
        "- Coherent sentences and/or paragraphs\n"
        "- Recognisable language (any language accepted)\n"
        "- No significant OCR artefacts\n\n"
        f"Text to evaluate:\n---\n{sample}\n---\n\n"
        "Return only the JSON object, no markdown fences."
    )

    response_text: Optional[str] = None
    try:
        provider = get_ai_provider()
        model = settings.ai_model or settings.openai_model or "gpt-4o-mini"
        response_text = provider.chat_completion(
            messages=[
                {
                    "role": "system",
                    "content": "You are a document quality assessor. Respond only with valid JSON.",
                },
                {"role": "user", "content": prompt},
            ],
            model=model,
            temperature=0,
        )

        logger.info(f"[text_quality] AI quality check raw response: {response_text[:500]}")

        # Strip optional markdown code fences before parsing.
        clean = re.sub(r"```(?:json)?\s*", "", response_text).strip().rstrip("`").strip()
        parsed: dict = json.loads(clean)

        quality_score = int(parsed.get("quality_score", 0))
        is_good_ai = bool(parsed.get("is_good_quality", quality_score >= threshold))
        feedback = str(parsed.get("feedback", ""))
        issues = list(parsed.get("issues", []))

        # Apply strict rules: reject when score is below threshold OR when any
        # significant issue is present (even if the AI says is_good_quality=true).
        score_ok = quality_score >= threshold
        has_significant_issue = bool(significant_issues and any(i in issues for i in significant_issues))

        is_good = score_ok and is_good_ai and not has_significant_issue

        return TextQualityResult(
            is_good_quality=is_good,
            quality_score=quality_score,
            text_source=text_source,
            feedback=feedback,
            issues=issues,
            ai_response_raw=response_text,
        )

    except json.JSONDecodeError as exc:
        logger.warning(
            f"[text_quality] Could not parse AI quality response as JSON: {exc}. "
            "Treating text as acceptable quality to avoid false negatives."
        )
        return TextQualityResult(
            is_good_quality=True,
            quality_score=50,
            text_source=text_source,
            feedback=f"AI response could not be parsed as JSON ({exc}); assuming acceptable quality.",
            ai_response_raw=response_text,
        )
