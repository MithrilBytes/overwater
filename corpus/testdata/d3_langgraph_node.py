from langchain_anthropic import ChatAnthropic
from langchain_core.tools import tool


@tool
def lookup_invoice(invoice_id: str) -> str:
    """Fetch an invoice by id."""
    return db.invoice(invoice_id)


@tool
def apply_credit(invoice_id: str, amount: float) -> str:
    """Apply a credit to an invoice."""
    return billing.credit(invoice_id, amount)


llm = ChatAnthropic(model="claude-sonnet-4-5", max_tokens=2048).bind_tools([lookup_invoice, apply_credit])


def agent_node(state: dict) -> dict:
    return {"messages": [llm.invoke(state["messages"])]}
