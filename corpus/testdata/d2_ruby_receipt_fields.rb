require "anthropic"

client = Anthropic::Client.new(api_key: ENV["ANTHROPIC_API_KEY"])

RECEIPT_PROMPT = "Copy the merchant, purchase date, subtotal, tax, and total off the receipt. Return one JSON object."

def receipt_fields(client, receipt_text)
  message = client.messages.create(
    model: "claude-haiku-4-5",
    max_tokens: 300,
    temperature: 0,
    system: RECEIPT_PROMPT,
    messages: [{ role: "user", content: receipt_text }]
  )
  message.content.first.text
end
