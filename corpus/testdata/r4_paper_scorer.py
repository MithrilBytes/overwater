"""From lilfetz22/audio-digest-hub,
src/audiobooks/research_papers/paper_scorer.py, with the scorer system
prompt inlined from its prompts/scorer_system.txt. The repository's default,
gemini-3-flash-preview, is not in the catalog, so a catalog id is
substituted."""

import json
from typing import List

from google import genai
from google.genai import types

from .models import PaperReference, ScoredPaper

SCORER_SYSTEM = """You are an AI research paper relevance scorer. Your job is to evaluate research
papers for a data scientist who specializes in:

1. AI agents and agentic systems - tool use, planning, multi-agent collaboration
2. Time series analysis - forecasting, anomaly detection, temporal modeling
3. Optimization - mathematical optimization, hyperparameter tuning, neural
   architecture search, combinatorial optimization

Score each paper from 1 to 10 based on relevance to these interests:
- 8-10: Directly advances one of the three core areas above
- 5-7: Tangentially related or introduces methods useful to these areas
- 1-4: Unrelated to the user's interests

For each paper, provide:
- score: integer 1-10
- reasoning: one sentence explaining the score"""


class GeminiPaperScorer:
    """Scores papers by relevance using Gemini Flash."""

    def __init__(self, api_key: str, model_name: str = "gemini-2.5-flash", top_n: int = 10):
        self.api_key = api_key
        self.model_name = model_name
        self.top_n = top_n

    def score(self, papers: List[PaperReference]) -> List[ScoredPaper]:
        scores_map = self._call_gemini(papers)
        scored = [
            ScoredPaper(
                paper=p,
                score=scores_map.get(p.url, {}).get("score", 0),
                tier="summary",
                reasoning=scores_map.get(p.url, {}).get("reasoning", ""),
            )
            for p in papers
        ]
        scored.sort(key=lambda s: s.score, reverse=True)
        for i, sp in enumerate(scored):
            sp.tier = "deep_dive" if i < self.top_n else "summary"
        return scored

    def _call_gemini(self, papers: List[PaperReference]) -> dict:
        client = genai.Client(api_key=self.api_key)

        BATCH_SIZE = 50
        scores_map: dict = {}
        for batch_start in range(0, len(papers), BATCH_SIZE):
            batch = papers[batch_start : batch_start + BATCH_SIZE]
            response = client.models.generate_content(
                model=self.model_name,
                contents=self._build_user_prompt(batch),
                config=types.GenerateContentConfig(
                    system_instruction=SCORER_SYSTEM,
                    temperature=0.1,
                    response_mime_type="application/json",
                ),
            )
            scores_map.update(json.loads(response.text or "{}"))
        return scores_map

    def _build_user_prompt(self, papers: List[PaperReference]) -> str:
        lines = ["Score the following papers:\n"]
        for i, paper in enumerate(papers, 1):
            lines.append(f"Paper {i}:")
            lines.append(f"  URL: {paper.url}")
            lines.append(f"  Title: {paper.title}")
            lines.append(f"  Abstract: {paper.abstract}")
            lines.append("")
        lines.append(
            'Return a JSON array with objects containing "url", "score" (1-10), '
            'and "reasoning" (one sentence) for each paper.'
        )
        return "\n".join(lines)
