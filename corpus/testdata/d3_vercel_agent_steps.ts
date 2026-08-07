import { anthropic } from "@ai-sdk/anthropic";
import { generateText, stepCountIs, tool } from "ai";
import { z } from "zod";

export async function runResearch(question: string) {
  const { text } = await generateText({
    model: anthropic("claude-opus-4-5"),
    system: "You are a research agent. Search, read, and keep going until you can answer with citations.",
    prompt: question,
    stopWhen: stepCountIs(12),
    maxOutputTokens: 4000,
    tools: {
      webSearch: tool({ description: "Search the web.", inputSchema: z.object({ query: z.string() }) }),
      fetchPage: tool({ description: "Fetch a URL.", inputSchema: z.object({ url: z.string() }) }),
    },
  });
  return text;
}
