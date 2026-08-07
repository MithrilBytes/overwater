import { anthropic } from "@ai-sdk/anthropic";
import { generateText } from "ai";

export async function writeModule(spec: string) {
  const { text } = await generateText({
    model: anthropic("claude-sonnet-5"),
    system: "Write the Terraform module for the described resource. Emit HCL only, no commentary.",
    prompt: spec,
    maxOutputTokens: 3000,
    temperature: 0.1,
  });
  return text;
}
