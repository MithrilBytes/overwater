import { GoogleGenAI } from "@google/genai";
import { readFile } from "node:fs/promises";

const ai = new GoogleGenAI({ apiKey: process.env.GEMINI_API_KEY });

export async function transcribeVoicemail(path: string) {
  const audio = await readFile(path);
  const result = await ai.models.generateContent({
    model: "gemini-2.5-flash",
    contents: [
      { inlineData: { mimeType: "audio/mp3", data: audio.toString("base64") } },
      { text: "Write out exactly what the caller says, with punctuation." },
    ],
  });
  return result.text;
}
