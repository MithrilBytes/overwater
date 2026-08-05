import { anthropic } from "@ai-sdk/anthropic";
import { streamText } from "ai";

export async function POST(req: Request) {
  const { messages } = await req.json();
  const result = streamText({
    model: anthropic("claude-opus-5"),
    system: "You are the Acme support assistant. Be warm and brief.",
    messages,
  });
  return result.toTextStreamResponse();
}
