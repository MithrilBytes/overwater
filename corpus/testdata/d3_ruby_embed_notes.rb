require "openai"

client = OpenAI::Client.new(api_key: ENV["OPENAI_API_KEY"])

def embed_notes(client, notes)
  response = client.embeddings.create(
    model: "text-embedding-3-small",
    input: notes
  )
  response.data.map(&:embedding)
end
