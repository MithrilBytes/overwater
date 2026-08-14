# From Kitware/vtk-mcp, src/vtk_mcp/tools/dsl.py with the translate_model
# default from src/vtk_mcp/config.py, which is where the repository keeps
# it. The call is a litellm one inside vtk_validate.dsl.

from __future__ import annotations

from typing import TYPE_CHECKING, Optional

from pydantic_settings import BaseSettings

if TYPE_CHECKING:
    from ..composition import VTKMCPContext


class Settings(BaseSettings):
    knowledge_artifact_path: Optional[str] = None
    vtk_version: str = "9.3.0"

    # DSL translation
    translate_model: str = "anthropic/claude-haiku-4-5"
    translate_base_url: Optional[str] = None  # e.g. http://localhost:11434 for Ollama
    translate_api_key: Optional[str] = None  # set to "ollama" for Ollama

    class Config:
        env_prefix = "VTK_MCP_"


def translate_prompt_to_dsl(
    query: str,
    ctx: "VTKMCPContext",
    model: str | None = None,
    base_url: str | None = None,
    api_key: str | None = None,
) -> str:
    """Translate a natural language VTK request into the pipeline DSL.

    Uses vtk-validate's DSL translator with the loaded api_index for class
    context. All parameters fall back to VTK_MCP_TRANSLATE_* env vars when
    not provided.

    Args:
        query: Natural language description (e.g. "create a warped sine surface").
        model: LiteLLM model identifier. Overrides VTK_MCP_TRANSLATE_MODEL when set.

    Returns:
        A VTK pipeline DSL string ready for code generation.
    """
    from vtk_validate.dsl import translate_to_dsl

    return translate_to_dsl(
        query=query,
        api_index=ctx.api_index,
        model=model or ctx.settings.translate_model,
        base_url=base_url or ctx.settings.translate_base_url,
        api_key=api_key or ctx.settings.translate_api_key,
    )
