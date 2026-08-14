# From Kitware/vtk-mcp, rag-components/chat.py. The model id is the
# repository's own --model default. The Anthropic and Ollama branches of
# init() are dropped so one model string is left.

from llama_index.core.llms import ChatMessage
from llama_index.llms.openai import OpenAI
from string import Template

import query_db

PROMPT = Template(
    """
You are an AI assistant specializing in VTK (Visualization Toolkit)
documentation. Your primary task is to provide accurate, concise, and helpful
responses to user queries about VTK, including relevant code snippets

Here is the context information you should use to answer queries:
<context>
$CONTEXT
</context>

Here's the user's query:

<user_query>
$QUERY
</user_query>

When responding to a user query, follow these guidelines:

1. Relevance Check:

   - If the query is not relevant to VTK, respond with "This question is not relevant to VTK."

2. Answer Formulation:

   - If you don't know the answer, clearly state that.
   - If uncertain, ask the user for clarification.
   - Respond in the same language as the user's query.
   - Be concise while providing complete information.
   - If the answer isn't in the context but you have the knowledge, explain this to the user and provide the answer based on your understanding.
"""
)

HISTORY = [
    ChatMessage(role="system", content="You are a helpful assistant"),
]

llm = None
client = None


def init(model: str = "gpt-4", database: str = "./db") -> None:
    global llm, client
    llm = OpenAI(model=model)
    client = query_db.initialize_db(database_path=database)


def ask(query: str, collection_name: str, top_k: int, streaming: bool = False):
    HISTORY.append(ChatMessage(role="user", content=query))

    results = query_db.query_db(query, collection_name, top_k, client)
    snippets = [item for item in results["code_documents"]]
    retrieved_text = "\n\n## Next example:\n\n".join(snippets)
    content = PROMPT.substitute(CONTEXT=retrieved_text, QUERY=query.rstrip())

    HISTORY.append(ChatMessage(role="assistant", content=content.rstrip()))

    if streaming:
        response = llm.stream_chat(HISTORY)
    else:
        response = llm.chat(HISTORY)

    return {"response": response}
