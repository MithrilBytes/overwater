require "openai"

client = OpenAI::Client.new(api_key: ENV["OPENAI_API_KEY"])

TOOLS = [
  { type: "function", function: { name: "search_catalog", description: "Search the store catalog." } },
  { type: "function", function: { name: "add_to_cart", description: "Add an item to the cart." } }
]

def take_step(client, scratchpad)
  client.chat.completions.create(
    model: "gpt-4.1",
    max_tokens: 2000,
    tools: TOOLS,
    messages: scratchpad
  )
end
