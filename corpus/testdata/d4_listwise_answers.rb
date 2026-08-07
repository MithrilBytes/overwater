require "openai"

client = OpenAI::Client.new(api_key: ENV["OPENAI_API_KEY"])

def order_answers(client, question, answers)
  response = client.chat.completions.create(
    model: "gpt-4.1-nano",
    temperature: 0,
    max_tokens: 80,
    messages: [
      { role: "system", content: "Rank the candidate answers from most to least useful for the question. Reply with their numbers in order." },
      { role: "user", content: "#{question}\n#{answers}" }
    ]
  )
  response.choices.first.message.content
end
