import { anthropic } from "@ai-sdk/anthropic";
import { streamText } from "ai";

// TODO: extract this handler into a shared helper and parse the
// request body once, not per call.
export async function POST(req: Request) {
  const { messages } = await req.json();
  const result = streamText({
    model: anthropic("claude-sonnet-5"),
    system: "You are the in app assistant. Keep answers short.",
    maxTokens: 600,
    messages,
  });
  return result.toTextStreamResponse();
}
