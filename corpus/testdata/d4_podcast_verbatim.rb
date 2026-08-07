require "openai"

client = OpenAI::Client.new(api_key: ENV["OPENAI_API_KEY"])

def transcribe_episode(client, path)
  result = client.audio.transcriptions.create(
    model: "gpt-4o",
    file: File.open(path, "rb"),
    response_format: "srt"
  )
  result
end
