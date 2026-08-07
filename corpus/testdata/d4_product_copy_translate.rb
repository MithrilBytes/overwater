require "anthropic"

client = Anthropic::Client.new(api_key: ENV["ANTHROPIC_API_KEY"])

def translate_copy(client, copy, target)
  message = client.messages.create(
    model: "claude-haiku-4-5",
    max_tokens: 1000,
    temperature: 0,
    system: "Translate the product description into the target language. Keep the measurements and the brand name unchanged.",
    messages: [{ role: "user", content: "#{target}\n#{copy}" }]
  )
  message.content.first.text
end
