require "anthropic"

client = Anthropic::Client.new(api_key: ENV["ANTHROPIC_API_KEY"])

def moderate_post(client, post)
  message = client.messages.create(
    model: "claude-haiku-4-5",
    max_tokens: 10,
    system: "You are the content moderation filter. Reply allow or block for the post.",
    messages: [{ role: "user", content: post }]
  )
  message.content.first.text.strip == "block" ? :blocked : :allowed
end
