import { openai } from "@ai-sdk/openai";
import { generateObject } from "ai";
import { z } from "zod";

const Terms = z.object({
  counterparty: z.string(),
  effectiveDate: z.string(),
  terminationNotice: z.string(),
  governingLaw: z.string(),
});

export async function readContractTerms(contract: string) {
  const { object } = await generateObject({
    model: openai("gpt-4.1"),
    schema: Terms,
    temperature: 0,
    maxOutputTokens: 500,
    prompt: `Copy these terms out of the agreement verbatim:\n\n${contract}`,
  });
  return object;
}
