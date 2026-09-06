"""From mrwadams/takedown-gpt, app.py. The Streamlit chrome is trimmed; the
tools, the kwargs builder, the agent build and the invoke are as written.
The repository's model list (gpt-5.6-luna, gpt-5.6-terra, gpt-5.6-sol) is
not in the catalog, so it is collapsed to one catalog id from the same
family, and with it the non reasoning branch of build_llm_kwargs, whose
prefix test would otherwise be a second model string."""

import streamlit as st
import whois
import whoisit
from ddgs import DDGS
from langchain.agents import create_agent
from langchain_core.tools import tool
from langchain_openai import ChatOpenAI

# Available OpenAI models (must be tool-calling models - the agent depends on it)
model_options = [
    "gpt-5.1",
]

# Reason options for the takedown request
reason_options = [
    "Copyright infringement",
    "Trademark infringement",
    "Defamation or libel",
    "Privacy violations",
    "Malware or phishing activities",
    "Violation of terms of service",
    "Personal safety concerns",
    "Other (specify)",
]

# Protocol options for performing domain lookups
lookup_options = [
    "WHOIS",
    "RDAP"
]

# Marker the model is instructed to emit when it cannot find a takedown email
NOT_FOUND_MARKER = "Email address for takedown requests: [not found]"

# Prompt template for the agent
PROMPT_TEMPLATE = """
Task:

1. Use the {tool_name} tool to find the domain registrar for {domain}.
2. Use the web_search tool to find the email address for takedown requests for that domain registrar.
3. Prepare a draft email takedown request to the hosting provider citing the following reason: {reason}. Include the additional information provided: {additional_info}

Your response must be in the following format and should not include any other information:

  - Registrar name: [registrar]
  - Email address for takedown requests: [registrar_email]
  - Email subject: [subject]
  - Email body: [body]

Your response:
"""

# System prompt for the agent
SYSTEM_PROMPT = (
    "You are an assistant that helps draft domain takedown requests. "
    "Use the available tools to look up the domain's registrar and to find "
    "the correct abuse/takedown contact email address before drafting."
)


# Agent tools
@tool
def get_registrar(domain: str) -> str:
    """Find the registrar of a domain name using a WHOIS lookup."""
    return whois.whois(domain).registrar


@tool
def rdap_lookup(domain: str) -> str:
    """Find the registrar of a domain name using an RDAP lookup."""
    whoisit.bootstrap()
    return str(whoisit.domain(domain))


@tool
def web_search(query: str) -> str:
    """Search the web for information. Ask targeted questions; useful for finding a
    domain registrar's abuse or takedown-request contact email address."""
    results = DDGS().text(query, max_results=5)
    if not results:
        return "No results found."
    return "\n\n".join(
        f"{r.get('title', '')}\n{r.get('href', '')}\n{r.get('body', '')}"
        for r in results
    )


def build_llm_kwargs(model, api_key):
    """Build the ChatOpenAI kwargs for the given model.

    GPT-5 reasoning models must use the Responses API to combine function tools
    with reasoning (the Chat Completions endpoint rejects that pairing with a 400),
    and they only support the default temperature.
    """
    return {"model": model, "api_key": api_key, "use_responses_api": True}


def select_lookup_tool(selected_lookup):
    """Map the chosen protocol to its (tool, prompt_name) pair, kept in lock-step."""
    if selected_lookup == "RDAP":
        return rdap_lookup, "rdap_lookup"
    return get_registrar, "get_registrar"


def extract_reply(result):
    """Read the agent's final reply as a string.

    Uses `.text` (not `.content`): the Responses API can return content as a list
    of blocks, and `.text` flattens either form to a plain string.
    """
    return result["messages"][-1].text


def is_email_missing(response):
    """True when the model reported it could not find a takedown email address."""
    return NOT_FOUND_MARKER in response


def main():
    api_key = st.sidebar.text_input("Enter your OpenAI API key:", type="password")

    # Model selection
    selected_model = st.sidebar.selectbox(
        "Select the OpenAI model you would like to use:",
        model_options,
        help="Select from the latest OpenAI chat models"
    )

    domain = st.text_input("Enter the domain that is the subject of the takedown request:", help="e.g. 'example.com'")
    reason = st.selectbox("Select the reason for the takedown request:", reason_options)
    custom_reason = None
    if reason == "Other (specify)":
        custom_reason = st.text_input("Specify the custom reason for the takedown request:")
    additional_info = st.text_area("Provide additional information to support your request (optional):")
    selected_lookup = st.selectbox("Select your preferred protocol for domain registrar lookups:", lookup_options)

    if st.button("Generate Takedown Request"):
        llm = ChatOpenAI(**build_llm_kwargs(selected_model, api_key))

        # Select the registrar-lookup tool for the chosen protocol, alongside search
        lookup_tool, tool_name = select_lookup_tool(selected_lookup)
        tools = [lookup_tool, web_search]

        # Build the agent (LangChain 1.x create_agent runs a tool-calling loop)
        open_ai_agent = create_agent(llm, tools=tools, system_prompt=SYSTEM_PROMPT)

        # Fill placeholders with actual data
        prompt_filled = PROMPT_TEMPLATE.format(
            tool_name=tool_name,
            domain=domain,
            reason=custom_reason if custom_reason else reason,
            additional_info=additional_info,
        )

        # Run the agent
        result = open_ai_agent.invoke(
            {"messages": [{"role": "user", "content": prompt_filled}]}
        )
        response = extract_reply(result)

        if is_email_missing(response):
            st.error("Could not find the email address for takedown requests. Please try again or manually search for the domain registrar's contact information.")
        else:
            st.code(response, language="text")


if __name__ == "__main__":
    main()
