"""From RobbyLinson/CheckEmailLight, src/briefing.py with the InboxDigest
schema inlined from src/schema.py. The per email sentiment signal the
digest reads comes from a local transformers pipeline (src/sentiment.py),
not a model call, so the digest is the only site."""

from typing import Literal

from langchain_anthropic import ChatAnthropic
from langchain_core.prompts import ChatPromptTemplate
from pydantic import BaseModel, Field


class InboxDigest(BaseModel):
    """A catch-up digest of an inbox after time away, prioritizing what needs attention."""

    overall_summary: str = Field(
        description="A two to three sentence plain-language summary of what happened "
                    "across the inbox while the user was away. Lead with the overall "
                    "tenor (calm, busy, a few fires) and call out anything urgent."
    )
    needs_attention: list[str] = Field(
        description="Emails that genuinely need the user's attention, each as a short "
                    "line naming the sender and why it matters. Order by urgency, most "
                    "pressing first. Leave empty if nothing needs attention."
    )
    todos: list[str] = Field(
        description="Concrete action items the user should take, phrased as clear tasks "
                    "(e.g. 'Reply to Marcus about invoice #4471'). Consolidate related "
                    "items. Leave empty if there are no actions."
    )
    can_ignore: list[str] = Field(
        description="Emails that need no action (spam, FYIs, newsletters), each as a "
                    "brief line, so the user knows they were reviewed and safely skipped."
    )
    urgency_level: Literal["low", "medium", "high"] = Field(
    description="Overall urgency of the inbox. 'high' if anything needs a same-day "
                "response or something is escalating; 'medium' if there are real "
                "to-dos but nothing time-critical; 'low' if it's all FYIs and noise."
)


model = ChatAnthropic(model_name="claude-haiku-4-5-20251001", timeout=None, stop=None)
structured_model = model.with_structured_output(InboxDigest)

prompt = ChatPromptTemplate.from_messages([
    ("system",
     "You catch someone up on their inbox after time away. "
     "Given a list of emails, each with a sentiment signal, produce a single "
     "digest: an overall summary of what happened, which emails need attention "
     "and why, and a consolidated to-do list."),
    ("human", "Here are the emails with sentiment signals:\n\n{emails}")
])
chain = prompt | structured_model

def generate_briefing(analyzed_emails: list):
    result = chain.invoke({"emails": analyzed_emails})
    return result
