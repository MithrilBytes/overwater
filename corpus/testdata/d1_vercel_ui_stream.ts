import { openai } from "@ai-sdk/openai";
import { streamText } from "ai";

export async function POST(req: Request) {
  const { messages } = await req.json();
  const result = streamText({
    model: openai("gpt-5-nano"),
    system: "You are the Acme docs assistant. Answer in short paragraphs and link the doc you used.",
    messages,
    maxOutputTokens: 800,
  });
  return result.toUIMessageStreamResponse();
}
