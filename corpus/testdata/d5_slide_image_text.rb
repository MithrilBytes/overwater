require "anthropic"

client = Anthropic::Client.new(api_key: ENV["ANTHROPIC_API_KEY"])

def slide_text(client, slide_b64)
  message = client.messages.create(
    model: "claude-haiku-4-5",
    max_tokens: 800,
    system: "You read slide images and return the text on them in reading order.",
    messages: [{ role: "user", content: [{ type: "image", source: { type: "base64", media_type: "image/png", data: slide_b64 } }] }]
  )
  message.content.first.text
end
