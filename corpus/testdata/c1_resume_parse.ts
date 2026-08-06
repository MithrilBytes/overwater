import { google } from "@ai-sdk/google";
import { generateObject } from "ai";
import { z } from "zod";

const Candidate = z.object({
  full_name: z.string(),
  email: z.string(),
  years_experience: z.number(),
  skills: z.array(z.string()),
});

export async function parseResume(resumeText: string) {
  const { object } = await generateObject({
    model: google("gemini-2.5-pro"),
    schema: Candidate,
    prompt: `Pull the candidate details from this resume:\n${resumeText}`,
  });
  return object;
}
