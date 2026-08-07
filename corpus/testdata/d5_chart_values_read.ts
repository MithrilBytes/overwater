import Anthropic from "@anthropic-ai/sdk";

const client = new Anthropic();

export async function readChart(pngBase64: string) {
  const message = await client.messages.create({
    model: "claude-sonnet-5",
    max_tokens: 600,
    messages: [
      {
        role: "user",
        content: [
          { type: "image", source: { type: "base64", media_type: "image/png", data: pngBase64 } },
          { type: "text", text: "What series are plotted here and what are the end of quarter values?" },
        ],
      },
    ],
  });
  return message.content[0].text;
}
