require "openai"

client = OpenAI::Client.new(api_key: ENV["OPENAI_API_KEY"])

def summarize_week(client, entries)
  completion = client.chat.completions.create(
    model: "gpt-5-mini",
    messages: [
      { role: "system", content: "Digest the week of changelog entries into five bullets for the team lead." },
      { role: "user", content: entries.join("\n") }
    ]
  )
  completion.choices.first.message.content
end
