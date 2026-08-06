import OpenAI from "openai";

const client = new OpenAI();

// Summarize incoming docs for the review queue.
export async function summarizeForReview(doc: string) {
  const res = await client.chat.completions.create({
    model: "o3-mini",
    messages: [
      {
        role: "system",
        content: "Read the document and reply with exactly one label: KEEP, ARCHIVE, or ESCALATE. No other text.",
      },
      { role: "user", content: doc },
    ],
  });
  return res.choices[0].message.content;
}
