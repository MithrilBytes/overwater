import { openai } from "@ai-sdk/openai";
import { streamText } from "ai";

export async function POST(req: Request) {
  const { messages } = await req.json();
  const result = streamText({
    model: openai("gpt-5.1"),
    system: "You are a friendly product assistant for the Lumen app.",
    messages,
  });
  return result.toTextStreamResponse();
}
