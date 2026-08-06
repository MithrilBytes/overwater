import { GoogleGenAI, Type } from "@google/genai";

const ai = new GoogleGenAI({ apiKey: process.env.GEMINI_API_KEY });

export async function categorizeLead(formNotes: string): Promise<string> {
  const response = await ai.models.generateContent({
    model: "gemini-2.5-flash-lite",
    contents: formNotes,
    config: {
      responseMimeType: "text/x.enum",
      responseSchema: { type: Type.STRING, enum: ["hot", "warm", "cold", "spam"] },
    },
  });
  return response.text ?? "cold";
}
