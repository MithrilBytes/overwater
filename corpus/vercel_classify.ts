import { anthropic } from "@ai-sdk/anthropic";
import { generateObject } from "ai";
import { z } from "zod";

const Feedback = z.object({
  sentiment: z.enum(["positive", "neutral", "negative"]),
});

export async function classifyFeedback(text: string) {
  const { object } = await generateObject({
    model: anthropic("claude-haiku-4-5"),
    temperature: 0,
    maxTokens: 60,
    schema: Feedback,
    prompt: text,
  });
  return object.sentiment;
}
