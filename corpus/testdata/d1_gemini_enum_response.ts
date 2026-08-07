import { GoogleGenAI, Type } from "@google/genai";

const ai = new GoogleGenAI({});

export async function tagArticle(article: string) {
  const result = await ai.models.generateContent({
    model: "gemini-2.0-flash-lite",
    contents: article,
    config: {
      responseMimeType: "text/x.enum",
      responseSchema: { type: Type.STRING, enum: ["policy", "research", "product", "opinion"] },
      temperature: 0,
      maxOutputTokens: 16,
    },
  });
  return result.text;
}
