import { openai } from "@ai-sdk/openai";
import { streamText } from "ai";

export async function answerWithSources(question: string, passages: string[], history: unknown[]) {
  const result = streamText({
    model: openai("gpt-4.1"),
    system:
      "Answer the reader's question in a couple of friendly sentences using the passages given, and cite the source slug you used.",
    messages: [...history, { role: "user", content: `${passages.join("\n\n")}\n\n${question}` }],
    maxOutputTokens: 900,
  });
  return result.toUIMessageStreamResponse();
}
