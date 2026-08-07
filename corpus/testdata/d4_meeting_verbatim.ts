import Anthropic from "@anthropic-ai/sdk";

const client = new Anthropic();

export async function verbatimMinutes(audioBase64: string) {
  const message = await client.messages.create({
    model: "claude-sonnet-5",
    max_tokens: 8000,
    system: "Produce a verbatim transcript of the recording. Keep every word, mark speaker changes, do not condense.",
    messages: [{ role: "user", content: [{ type: "input_audio", data: audioBase64 }] }],
  });
  return message.content[0].text;
}
