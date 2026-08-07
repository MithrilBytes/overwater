from langchain_core.messages import SystemMessage
from langchain_openai import ChatOpenAI

llm = ChatOpenAI(model="gpt-4o", temperature=0.7, max_tokens=700)

PERSONA = SystemMessage(
    "You are the neighborhood library assistant. Chat naturally, recommend one book at a time, "
    "and ask what the reader liked about the last one."
)


def respond(history: list) -> str:
    return llm.invoke([PERSONA] + history).content
