from langchain_mistralai import ChatMistralAI
from pydantic import BaseModel


class PurchaseOrder(BaseModel):
    po_number: str
    supplier: str
    total_usd: float


llm = ChatMistralAI(model="mistral-large-2411", temperature=0)
extractor = llm.with_structured_output(PurchaseOrder)


def extract_purchase_order(email_body: str) -> PurchaseOrder:
    return extractor.invoke("Pull the purchase order fields out of this email:\n\n" + email_body)
