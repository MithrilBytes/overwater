"""From A-Hoier/Aula-AI.d, src/agent.py, with AVAILABLE_MODELS from config.py,
which is where the repository keeps it, and the Azure client from src/llm.py.
The research_agent branch is dropped, and the model list is cut to the id
api.py defaults to so one model string is left; the other four entries, two
of them Anthropic ids that skip the Azure wrapping, go with it."""

from __future__ import annotations as _annotations

from datetime import datetime

from openai import AsyncAzureOpenAI
from pydantic_ai import Agent, Tool
from pydantic_ai.exceptions import ModelHTTPError, UserError
from pydantic_ai.models.openai import OpenAIModel
from pydantic_ai.providers.openai import OpenAIProvider

from config import app_settings
from src.aula_client import AulaClient

AVAILABLE_MODELS: list[str] = [
    "gpt-4o",
]


AVAILABLE_AGENTS: list[str] = ["research_agent", "aula_agent"]

current_time = datetime.now().isoformat()
aula = AulaClient(app_settings().AULA_USER, app_settings().AULA_PWD.get_secret_value())


def get_async_openai_client() -> AsyncAzureOpenAI:
    """Callable function that allows the llm client to be instatiated from other scripts."""
    return AsyncAzureOpenAI(
        api_version=app_settings().API_VERSION,
        api_key=app_settings().AZURE_OPENAI_API_KEY.get_secret_value(),
        azure_endpoint=app_settings().AZURE_OPENAI_ENDPOINT,
    )


def create_agent(model: str, agent: str) -> Agent:
    """
    Create an agent with the given model name.
    Args:
        model_name (str): The name of the model to use.
    """

    if model not in AVAILABLE_MODELS:
        raise ValueError(f"Model {model} not in {AVAILABLE_MODELS}")
    if agent not in AVAILABLE_AGENTS:
        raise ValueError(f"Agent {agent} not in {AVAILABLE_AGENTS}")
    if not model.startswith("anthropic"):
        try:
            client = get_async_openai_client()
            model = OpenAIModel(model, provider=OpenAIProvider(openai_client=client))
        except Exception as e:
            raise ValueError(f"Error creating model {model}: {e}")
    if agent == "aula_agent":
        system_prompt = f"""current_time: {current_time}
You're a helpful research assistant. You're an expert in navigating the danish school communication system, Aula.
Only use the tools if the user is talking about the school, institution or about their kids.
Make sure to set the active child before using any of the tools (except for fetch_basic_data).
"""
        tools = [
            Tool(
                name="set_active_child",
                description="Set which child profile we’re operating on. Expects a single string argument: the child's name.",
                function=aula.set_active_child,
            ),
            Tool(
                name="fetch_basic_data",
                description="Return some basic info on all children’s {name: institution}.",
                function=aula.fetch_basic_data,
            ),
            Tool(
                name="fetch_daily_overview",
                description="Return today’s presence overview for the active child. Requires active child to be set.",
                function=aula.fetch_daily_overview,
            ),
            Tool(
                name="fetch_messages",
                description="Fetch the latest unread message for the active child. Requires active child to be set.",
                function=aula.fetch_messages,
            ),
            Tool(
                name="fetch_calendar",
                description="Fetch upcoming calendar events for the next N days. Expects an integer argument. Requires active child to be set.",
                function=aula.fetch_calendar,
            ),
        ]
    try:
        return Agent(
            model=model,
            # deps_type=ResearchDependencies,
            # output_type=ResearchResult,
            system_prompt=system_prompt,
            tools=tools,
        )
    except UserError as e:
        raise ValueError(f"Error creating agent {agent}: {e}")
    except ModelHTTPError as e:
        raise ValueError(f"Error creating model {model}: {e}")


async def get_response(query: str, model: str, agent: str) -> str:
    # pick your model at runtime:
    agent = create_agent(model, agent)

    # run!
    result = await agent.run(query)
    return result.output
