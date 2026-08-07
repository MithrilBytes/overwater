import { openai } from "@ai-sdk/openai";
import { generateText } from "ai";

export async function localizeString(source: string, locale: string) {
  const { text } = await generateText({
    model: openai("gpt-4.1-mini"),
    system:
      "Translate the UI string into the target locale. Keep placeholders like {count} and any HTML tags exactly as they appear.",
    prompt: `locale: ${locale}\n${source}`,
    temperature: 0,
    maxOutputTokens: 200,
  });
  return text;
}
