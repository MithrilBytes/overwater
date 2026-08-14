"""From lilfetz22/audio-digest-hub,
src/audiobooks/research_papers/wiki_engine/classifier.py. The repository's
default, gemini-3.1-flash-lite-preview, is not in the catalog, so a catalog
id is substituted. The prompt is the repository's fallback text, which is
also what prompts/classify_system.txt holds."""

import logging
from pathlib import Path
from typing import List

from pydantic import BaseModel, ConfigDict

from .models import ClassifiedSection
from .utils import build_response_format, load_prompt, parse_json_response

logger = logging.getLogger(__name__)

PROMPTS_DIR = Path(__file__).parent / "prompts"


class _SectionClassification(BaseModel):
    """Structured-output contract for a single section classification."""

    model_config = ConfigDict(extra="forbid")

    category: str
    title: str
    paper_urls: list[str]


_CLASSIFY_RESPONSE_FORMAT = build_response_format(
    "section_classification", _SectionClassification
)

_CLASSIFY_SYSTEM_FALLBACK = """You are a research paper classifier. Given a transcript section, classify it into one or more categories.

Return a JSON object with these fields:
- "category": The primary category (one of: "AI Architecture", "Hardware", "Benchmarking", "Optimization", "NLP", "Computer Vision", "Reinforcement Learning", "Robotics", "Time Series", "AI Agents", "Safety & Alignment", "Other")
- "title": A short descriptive title for this section (max 10 words)
- "paper_urls": Any URLs mentioned in the text (list of strings)

Respond ONLY with valid JSON, no markdown formatting."""


class TranscriptClassifier:
    """Classifies transcript sections into topic categories using an LLM."""

    def __init__(self, llm_client=None, model_name: str = "gemini-1.5-flash"):
        self.llm_client = llm_client
        self.model_name = model_name
        self._classify_prompt = load_prompt(
            PROMPTS_DIR, "classify_system.txt", _CLASSIFY_SYSTEM_FALLBACK
        )

    def classify(self, sections: List[str]) -> List[ClassifiedSection]:
        return [self._classify_single(section) for section in sections]

    def _llm_generate(self, user_prompt: str, system_prompt: str, response_format: dict | None = None):
        response = self.llm_client.models.generate_content(
            model=self.model_name,
            contents=user_prompt,
            config={"system_instruction": system_prompt},
        )
        return response.text

    def _classify_single(self, text: str) -> ClassifiedSection:
        data = parse_json_response(
            lambda: self._llm_generate(
                text[:4000], self._classify_prompt, _CLASSIFY_RESPONSE_FORMAT
            ),
            context="classify",
        )
        if data is None:
            return ClassifiedSection(text=text, category="Other", title="Unclassified")
        parsed = _SectionClassification.model_validate(data)
        return ClassifiedSection(
            text=text,
            category=parsed.category or "Other",
            title=parsed.title,
            paper_urls=parsed.paper_urls,
        )
