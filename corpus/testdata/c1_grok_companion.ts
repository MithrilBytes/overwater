import OpenAI from "openai";

const xai = new OpenAI({
  apiKey: process.env.XAI_API_KEY,
  baseURL: "https://api.x.ai/v1",
});

type Turn = { role: "user" | "assistant"; content: string };

export async function chatWithCompanion(history: Turn[]) {
  const completion = await xai.chat.completions.create({
    model: "grok-4",
    messages: [
      { role: "system", content: "You are Nova, a witty companion. Keep the conversation going." },
      ...history,
    ],
  });
  return completion.choices[0].message.content;
}
