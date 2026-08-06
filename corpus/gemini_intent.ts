import { GoogleGenAI } from "@google/genai";

const ai = new GoogleGenAI({});

export async function classifyIntent(utterance: string) {
  const result = await ai.models.generateContent({
    model: "gemini-2.5-flash",
    generationConfig: { temperature: 0, maxOutputTokens: 32 },
    contents: "Label the intent as order, refund, or question: " + utterance,
  });
  return result.text;
}
