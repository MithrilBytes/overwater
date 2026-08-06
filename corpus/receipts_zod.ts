import { openai } from "@ai-sdk/openai";
import { generateObject } from "ai";
import { z } from "zod";

const Receipt = z.object({
  merchant: z.string(),
  total_cents: z.number(),
  purchased_at: z.string(),
});

export async function parseReceipt(image_text: string) {
  const { object } = await generateObject({
    model: openai("gpt-5-mini"),
    temperature: 0,
    maxTokens: 200,
    schema: Receipt,
    prompt: image_text,
  });
  return object;
}
