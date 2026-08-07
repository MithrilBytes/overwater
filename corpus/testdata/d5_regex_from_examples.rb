require "openai"

client = OpenAI::Client.new(api_key: ENV["OPENAI_API_KEY"])

def build_regex(client, examples)
  response = client.chat.completions.create(
    model: "gpt-4.1-mini",
    max_tokens: 200,
    temperature: 0,
    messages: [
      { role: "system", content: "Write a Ruby regular expression that matches every positive example and none of the negative ones. Output the regex only." },
      { role: "user", content: examples }
    ]
  )
  response.choices.first.message.content
end
