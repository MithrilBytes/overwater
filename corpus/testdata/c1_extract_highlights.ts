import { openai } from "@ai-sdk/openai";
import { generateText } from "ai";

// Extract the highlights callers mention most.
export async function extractHighlights(callLog: string) {
  const { text } = await generateText({
    model: openai("gpt-4o-mini"),
    prompt: `Write a short paragraph that sums up this support call, covering the caller's mood and the resolution:\n\n${callLog}`,
    maxOutputTokens: 180,
  });
  return text;
}
