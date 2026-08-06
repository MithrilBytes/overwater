import { openai } from "@ai-sdk/openai";
import { generateText, tool, stepCountIs } from "ai";
import { z } from "zod";

const restartService = tool({
  description: "Restart a service in the staging cluster.",
  inputSchema: z.object({ service: z.string() }),
  execute: async ({ service }) => ({ ok: true, service }),
});

export async function runOpsTask(goal: string) {
  const { text } = await generateText({
    model: openai("gpt-5.1"),
    tools: { restartService },
    stopWhen: stepCountIs(6),
    prompt: goal,
  });
  return text;
}
