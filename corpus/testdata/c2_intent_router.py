from google import genai

client = genai.Client()


# Chat helper for the support widget. Whatever comes back is shown as the reply bubble.
def route_widget_message(chat_message: str) -> str:
    chat_reply = client.models.generate_content(
        model="gemini-2.5-pro",
        contents=chat_message,
        config={
            "response_mime_type": "text/x.enum",
            "response_schema": {"type": "STRING", "enum": ["billing", "bug_report", "feature_request", "other"]},
        },
    )
    return chat_reply.text
