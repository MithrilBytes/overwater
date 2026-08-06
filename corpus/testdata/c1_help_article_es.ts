import { mistral } from "@ai-sdk/mistral";
import { generateText } from "ai";

export async function translateHelpArticle(markdown: string, locale: string) {
  const { text } = await generateText({
    model: mistral("mistral-small-2506"),
    system: `Translate the help article into ${locale}. Preserve the markdown structure.`,
    prompt: markdown,
  });
  return text;
}
