"""From christianlouis/DocuElevate, app/tasks/extract_metadata_with_gpt.py,
with the OpenAI branch of the provider wrapper from app/utils/ai_provider.py
and the openai_model default from app/config.py, which is where the
repository keeps it. The other six providers, the temperature coercion for
reasoning models (its prefix test is another model string) and the token
sampling of long documents are dropped. The prompt is as written."""

import json
import logging
import os
import re
from typing import Any, Dict, List, Optional

import openai
from pydantic_settings import BaseSettings

from app.celery_app import celery
from app.tasks.retry_config import BaseTaskWithRetry
from app.utils import log_task_progress

logger = logging.getLogger(__name__)


class Settings(BaseSettings):
    openai_api_key: str = ""
    openai_base_url: str = "https://api.openai.com/v1"  # Default to OpenAI's endpoint
    openai_model: str = "gpt-4o-mini"  # Default model

    # AI provider abstraction layer
    # Supported values: openai, azure, anthropic, gemini, ollama, openrouter, litellm
    ai_provider: str = "openai"
    # Override model for any provider; falls back to openai_model when not set
    ai_model: Optional[str] = None


settings = Settings()


class OpenAIProvider:
    """OpenAI provider using the ``openai`` Python SDK.

    Also works as a drop-in for any OpenAI-compatible API endpoint, including
    LocalAI and LM Studio.
    """

    def __init__(self, api_key: str, base_url: Optional[str] = None) -> None:
        self._client = openai.OpenAI(
            api_key=api_key,
            base_url=base_url or "https://api.openai.com/v1",
        )

    def chat_completion(
        self,
        messages: List[Dict[str, str]],
        model: str,
        temperature: float = 0,
        **kwargs: Any,
    ) -> str:
        call_kwargs: Dict[str, Any] = {"model": model, "messages": messages, "temperature": temperature}
        call_kwargs.update(kwargs)
        completion = self._client.chat.completions.create(**call_kwargs)
        return completion.choices[0].message.content


def get_ai_provider() -> OpenAIProvider:
    """Factory function that creates and returns the configured AI provider."""
    return OpenAIProvider(
        api_key=settings.openai_api_key,
        base_url=settings.openai_base_url,
    )


def extract_json_from_text(text):
    """
    Try to extract a JSON object from the text.
    - First, check for a JSON block inside triple backticks.
    - If not found, try to extract text from the first '{' to the last '}'.
    """
    pattern = r"```(?:json)?\s*(\{.*?\})\s*```"
    match = re.search(pattern, text, re.DOTALL)
    if match:
        return match.group(1)
    else:
        start = text.find("{")
        end = text.rfind("}")
        if start != -1 and end != -1 and end > start:
            return text[start : end + 1]
    return None


@celery.task(base=BaseTaskWithRetry, bind=True)
def extract_metadata_with_gpt(self, filename: str, cleaned_text: str, file_id: int = None):
    """
    Uses OpenAI to classify document metadata.

    Args:
        filename: Can be either a basename (e.g., "file.pdf") or a full path (e.g., "/workdir/processed/file.pdf")
        cleaned_text: The extracted text from the document
        file_id: Optional file ID for tracking
    """
    task_id = self.request.id
    logger.info(f"[{task_id}] Starting metadata extraction for: {filename}")

    model = settings.ai_model or settings.openai_model
    metadata_text = cleaned_text

    prompt = (
        "You are a specialized document analyzer trained to extract structured metadata from documents.\n"
        "Your task is to analyze the given text and return a well-structured JSON object.\n\n"
        "Extract and return the following fields:\n"
        "1. **filename**: Machine-readable filename "
        "(YYYY-MM-DD_DescriptiveTitle, use only letters, numbers, periods, and underscores).\n"
        '2. **empfaenger**: The recipient, or "Unknown" if not found.\n'
        '3. **absender**: The sender, or "Unknown" if not found.\n'
        "4. **correspondent**: The entity or company that issued the document "
        '(shortest possible name, e.g., "Amazon" instead of "Amazon EU SARL, German branch").\n'
        "5. **kommunikationsart**: One of [Behoerdlicher_Brief, Rechnung, Kontoauszug, Vertrag, "
        "Quittung, Privater_Brief, Einladung, Gewerbliche_Korrespondenz, Newsletter, Werbung, Sonstiges].\n"
        "6. **kommunikationskategorie**: One of [Amtliche_Postbehoerdliche_Dokumente, "
        "Finanz_und_Vertragsdokumente, Geschaeftliche_Kommunikation, "
        "Private_Korrespondenz, Sonstige_Informationen].\n"
        "7. **document_type**: Precise classification (e.g., Invoice, Contract, Information, Unknown).\n"
        "8. **tags**: A list of up to 4 relevant thematic keywords.\n"
        '9. **language**: Detected document language (ISO 639-1 code, e.g., "de" or "en").\n'
        "10. **title**: A human-readable title summarizing the document content.\n"
        "11. **confidence_score**: A numeric value (0-100) indicating the confidence level "
        "of the extracted metadata.\n"
        "12. **reference_number**: Extracted invoice/order/reference number if available.\n"
        "13. **monetary_amounts**: A list of key monetary values detected in the document.\n\n"
        "### Important Rules:\n"
        "- **OCR Correction**: Assume the text has been corrected for OCR errors.\n"
        "- **Tagging**: Max 4 tags, avoiding generic or overly specific terms.\n"
        "- **Title**: Concise, no addresses, and contains key identifying features.\n"
        "- **Date Selection**: Prefer an explicit document-level date in the header "
        "(such as report date, invoice date, or letter date) over line-item, due, completion, "
        "or historical dates. Infer ambiguous numeric date order from unambiguous dates elsewhere "
        "in the same document.\n"
        "- **Document Parties**: Extract sender and recipient only from document-level authorship or "
        "addressing. Do not use a report title, scope, or arbitrary person from a table as a party.\n"
        "- **Output Language**: Maintain the document's original language.\n\n"
        f"Extracted text:\n{metadata_text}\n\n"
        "Return only valid JSON with no additional commentary.\n"
    )

    try:
        logger.info(f"[{task_id}] Sending classification request for {filename}...")
        log_task_progress(task_id, "call_ai_provider", "in_progress", "Calling AI provider API", file_id=file_id)
        provider = get_ai_provider()
        content = provider.chat_completion(
            messages=[
                {"role": "system", "content": "You are an intelligent document classifier."},
                {"role": "user", "content": prompt},
            ],
            model=model,
            temperature=0,
        )

        logger.info(f"[{task_id}] Raw classification response for {filename}: {content[:200]}...")

        json_text = extract_json_from_text(content)
        if not json_text:
            logger.error(f"[{task_id}] Could not find valid JSON in GPT response for {filename}.")
            return {}

        metadata = json.loads(json_text)
        return {"s3_file": os.path.basename(filename), "metadata": metadata}
    except Exception as e:
        logger.error(f"[{task_id}] OpenAI classification failed for {filename}: {e}")
        return {}
