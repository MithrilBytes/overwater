require "openai"

client = OpenAI::Client.new(api_key: ENV["OPENAI_API_KEY"])

def thread_brief(client, messages)
  response = client.chat.completions.create(
    model: "gpt-4.1-mini",
    max_tokens: 400,
    messages: [
      { role: "system", content: "Sum up this email thread for someone joining it late. One paragraph, no greeting." },
      { role: "user", content: messages.join("\n\n") }
    ]
  )
  response.choices.first.message.content
end
