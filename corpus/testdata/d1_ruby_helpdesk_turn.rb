require "openai"

client = OpenAI::Client.new(api_key: ENV["OPENAI_API_KEY"])

def next_message(client, turns)
  response = client.chat.completions.create(
    model: "gpt-4o-mini",
    max_tokens: 600,
    temperature: 0.7,
    messages: [{ role: "system", content: "You are the studio booking assistant. Keep the conversation going and confirm details back to the caller." }] + turns
  )
  response.choices.first.message.content
end
