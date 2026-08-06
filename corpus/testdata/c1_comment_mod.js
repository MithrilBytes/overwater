import OpenAI from "openai";

const client = new OpenAI();

const POLICY = "No slurs, threats, doxxing, or spam links.";

export async function moderateComment(comment) {
  const verdict = await client.chat.completions.create({
    model: "gpt-5-nano",
    temperature: 0,
    max_completion_tokens: 5,
    messages: [
      { role: "system", content: `Policy: ${POLICY} Answer ALLOW or BLOCK only.` },
      { role: "user", content: comment },
    ],
  });
  return verdict.choices[0].message.content === "BLOCK";
}
