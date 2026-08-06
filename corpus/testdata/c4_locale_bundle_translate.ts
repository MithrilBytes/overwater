import OpenAI from "openai";

const client = new OpenAI({
  baseURL: "https://api.deepseek.com",
  apiKey: process.env.DEEPSEEK_API_KEY,
});

export async function translateLocaleBundle(entries: Record<string, string>, locale: string) {
  const completion = await client.chat.completions.create({
    model: "deepseek-chat",
    temperature: 0,
    messages: [
      {
        role: "system",
        content:
          "Translate each UI string value into " +
          locale +
          ". Keep placeholders like {name} untouched. Return the same JSON shape.",
      },
      { role: "user", content: JSON.stringify(entries) },
    ],
  });
  return JSON.parse(completion.choices[0].message.content ?? "{}");
}
