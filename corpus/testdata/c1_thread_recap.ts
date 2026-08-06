import OpenAI from "openai";

const client = new OpenAI();

export async function recapThread(emails: string[]) {
  const response = await client.responses.create({
    model: "gpt-5",
    instructions: "Summarize the email thread in at most three bullet points.",
    input: emails.join("\n---\n"),
  });
  return response.output_text;
}
