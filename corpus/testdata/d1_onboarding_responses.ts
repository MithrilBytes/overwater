import OpenAI from "openai";

const client = new OpenAI();

export async function onboardingTurn(input: string) {
  const response = await client.responses.create({
    model: "gpt-5-mini",
    instructions:
      "You are the onboarding buddy for new Acme users. Keep each reply under three sentences and end with a question.",
    input,
    max_output_tokens: 600,
  });
  return response.output_text;
}
