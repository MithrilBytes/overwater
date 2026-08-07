import { openai } from "@ai-sdk/openai";
import { generateObject } from "ai";
import { z } from "zod";

const Verdict = z.object({ violates_policy: z.boolean() });

export async function screenUpload(caption: string) {
  const { object } = await generateObject({
    model: openai("gpt-4.1-nano"),
    schema: Verdict,
    temperature: 0,
    maxOutputTokens: 20,
    system: "Decide whether the caption breaks the community policy on hate speech, nudity, or harassment.",
    prompt: caption,
  });
  return object.violates_policy;
}
