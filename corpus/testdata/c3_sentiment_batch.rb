require "openai"

client = OpenAI::Client.new(api_key: ENV["OPENAI_API_KEY"])

def classify_sentiment(client, review)
  completion = client.chat.completions.create(
    model: "gpt-4o-mini",
    messages: [
      { role: "system", content: "Label the review sentiment as positive, negative, or mixed. One word only." },
      { role: "user", content: review }
    ]
  )
  completion.choices.first.message.content.strip.downcase
end
