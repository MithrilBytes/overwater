import { anthropic } from "@ai-sdk/anthropic";
import { generateObject } from "ai";
import { z } from "zod";

const Intent = z.object({
  intent: z.enum(["cancel", "upgrade", "refund", "question"]),
});

export async function readIntent(message: string) {
  const { object } = await generateObject({
    model: anthropic("claude-haiku-4-5"),
    schema: Intent,
    temperature: 0,
    maxOutputTokens: 30,
    prompt: message,
  });
  return object.intent;
}
