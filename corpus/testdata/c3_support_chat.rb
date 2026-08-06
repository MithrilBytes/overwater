require "openai"

client = OpenAI::Client.new(
  access_token: ENV.fetch("DEEPSEEK_API_KEY"),
  uri_base: "https://api.deepseek.com"
)

def chat_reply(client, history)
  system_turn = { role: "system", content: "You are the storefront assistant. Answer in two sentences or fewer." }
  client.chat(
    parameters: {
      model: "deepseek-chat",
      messages: [system_turn] + history,
      stream: proc do |chunk|
        print chunk.dig("choices", 0, "delta", "content")
      end
    }
  )
end
