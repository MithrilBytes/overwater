import OpenAI from "openai";

const openai = new OpenAI();

export async function scoreSentiment(reviewText) {
  const out = await openai.chat.completions.create({
    model: "gpt-4.1-mini",
    temperature: 0,
    max_tokens: 3,
    messages: [
      { role: "system", content: "Label the review as positive, neutral, or negative. One word only." },
      { role: "user", content: reviewText },
    ],
  });
  return out.choices[0].message.content.trim().toLowerCase();
}
