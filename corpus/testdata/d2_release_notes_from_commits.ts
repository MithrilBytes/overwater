import { openai } from "@ai-sdk/openai";
import { generateText } from "ai";

export async function releaseNotes(commits: string[]) {
  const { text } = await generateText({
    model: openai("gpt-5-mini"),
    system:
      "Write the release notes for this version. Group the commits into themes, one sentence each, and skip merge commits.",
    prompt: commits.join("\n"),
    maxOutputTokens: 900,
  });
  return text;
}
