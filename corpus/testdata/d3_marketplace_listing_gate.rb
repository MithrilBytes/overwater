require "anthropic"

client = Anthropic::Client.new(api_key: ENV["ANTHROPIC_API_KEY"])

RULES = "Reject listings that break marketplace policy: counterfeits, weapons, live animals, or stolen goods. Reply approve or reject."

def review_listing(client, listing)
  message = client.messages.create(
    model: "claude-haiku-4-5",
    max_tokens: 4,
    temperature: 0,
    system: RULES,
    messages: [{ role: "user", content: listing }]
  )
  message.content.first.text.strip
end
