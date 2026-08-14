"""From lilfetz22/audio-digest-hub,
src/audiobooks/research_papers/feedback.py, PreferenceProfileManager. The
repository's default, gemini-3-flash-preview, is not in the catalog, so a
catalog id is substituted."""

import json
import logging
from typing import List

from google import genai
from google.genai import types

logger = logging.getLogger(__name__)


class PreferenceProfileManager:
    """Manages a cumulative preference profile stored as a local JSON file."""

    def __init__(
        self,
        profile_path: str = "preference_profile.json",
        api_key: str = "",
        model_name: str = "gemini-2.0-flash",
    ):
        self.profile_path = profile_path
        self.api_key = api_key
        self.model_name = model_name

    def _extract_interests(self, papers: List[dict]) -> List[str]:
        """Use Gemini Flash to extract interest patterns from clicked papers."""
        if not self.api_key or self.api_key == "your-gemini-api-key":
            return []

        titles_abstracts = "\n".join(
            f"- {p['title']}: {p.get('abstract', '')}" for p in papers
        )

        try:
            client = genai.Client(api_key=self.api_key)
            response = client.models.generate_content(
                model=self.model_name,
                contents=(
                    f"Extract 1-3 broad interest areas from these research papers "
                    f"the user clicked on:\n{titles_abstracts}\n\n"
                    f"Return a JSON array of short interest strings."
                ),
                config=types.GenerateContentConfig(
                    temperature=0.1,
                    response_mime_type="application/json",
                ),
            )

            text = response.text.strip()
            if text.startswith("```"):
                text = text.split("\n", 1)[1]
                if text.endswith("```"):
                    text = text[:-3]
                text = text.strip()

            interests = json.loads(text)
            if isinstance(interests, list):
                return [str(i) for i in interests]

        except Exception as e:
            logger.warning(f"Failed to extract interests: {e}")

        return []
