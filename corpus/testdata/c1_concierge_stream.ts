import { anthropic } from "@ai-sdk/anthropic";
import { streamText } from "ai";

export async function POST(req: Request) {
  const { messages } = await req.json();
  const result = streamText({
    model: anthropic("claude-sonnet-5"),
    system:
      "You are the concierge assistant for the Harborview hotel. Keep replies warm and brief.",
    messages,
    maxOutputTokens: 512,
  });
  return result.toUIMessageStreamResponse();
}
