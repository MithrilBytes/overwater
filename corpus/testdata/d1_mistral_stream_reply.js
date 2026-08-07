import { Mistral } from "@mistralai/mistralai";

const client = new Mistral({ apiKey: process.env.MISTRAL_API_KEY });

export async function streamReply(turns) {
  const result = await client.chat.stream({
    model: "mistral-small-2506",
    messages: [
      { role: "system", content: "You are a laid back cooking buddy. Reply in one short paragraph." },
      ...turns,
    ],
    maxTokens: 700,
  });
  return result;
}
