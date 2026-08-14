"""From lilfetz22/audio-digest-hub,
src/audiobooks/research_papers/transcript_generator.py. The repository's
default, gemini-3.1-flash-lite-preview, is not in the catalog, so a catalog
id is substituted. The system prompt stays where the repository keeps it,
in prompts/narrator_system.txt: it tells the model to narrate a research
digest segment about one paper."""

import logging
from pathlib import Path
from typing import List

from .gemini_client import GeminiClientWithFallback
from .models import PaperContent

logger = logging.getLogger(__name__)

PROMPTS_DIR = Path(__file__).parent / "prompts"

MAX_REQUESTS_PER_MINUTE = 15
MAX_TOKENS_PER_MINUTE = 1_000_000
CHARS_PER_TOKEN = 4


class GeminiTranscriptGenerator:
    """Generates podcast transcripts using Gemini.

    Each deep-dive paper is sent individually via sequential realtime API calls
    with rate limiting to stay within the free tier (15 RPM, 1M TPM).
    """

    def __init__(
        self,
        api_key: str,
        model_name: str = "gemini-1.5-flash",
        backup_api_key: str | None = None,
    ):
        self.model_name = model_name
        self._fallback_client = GeminiClientWithFallback(
            api_key=api_key,
            model_name=model_name,
            backup_api_key=backup_api_key,
        )
        self._request_timestamps: List[float] = []

    def generate(self, deep_dive_papers: List[PaperContent], date_str: str) -> str:
        system_prompt = self._load_system_prompt()

        deep_dive_transcripts: List[str] = []
        for i, paper in enumerate(deep_dive_papers):
            prompt = self._build_single_paper_prompt(paper, date_str)
            estimated_input_tokens = (len(system_prompt) + len(prompt)) // CHARS_PER_TOKEN
            self._wait_for_rate_limit(estimated_input_tokens)

            logger.info(
                f"Generating transcript for paper {i + 1}/{len(deep_dive_papers)}: {paper.title}"
            )
            deep_dive_transcripts.append(self._fallback_client.generate(prompt, system_prompt))

        return "\n\n".join(deep_dive_transcripts)

    def _load_system_prompt(self) -> str:
        """Load the narrator system prompt."""
        prompt_path = PROMPTS_DIR / "narrator_system.txt"
        return prompt_path.read_text(encoding="utf-8")

    def _build_single_paper_prompt(self, paper: PaperContent, date_str: str) -> str:
        """Build a user prompt for a deep dive into a single paper."""
        sections = [
            f"Generate a deep-dive research digest segment for {date_str}.\n",
            f"--- Paper: {paper.title} ---",
            f"Source: {paper.source}",
            f"URL: {paper.url}",
            f"Abstract: {paper.abstract}",
            f"Full Text:\n{paper.full_text}\n",
        ]
        return "\n".join(sections)

    def _wait_for_rate_limit(self, estimated_tokens: int) -> None:
        raise NotImplementedError
