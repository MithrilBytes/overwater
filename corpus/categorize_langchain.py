from langchain_openai import ChatOpenAI

llm = ChatOpenAI(model="gpt-4o-mini", temperature=0, max_tokens=50)


def categorize_ticket(body: str) -> str:
    result = llm.invoke(
        "Assign one label from billing, bug, or question.\n\n" + body
    )
    return result.content
