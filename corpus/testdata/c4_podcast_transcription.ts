import { GoogleGenAI } from "@google/genai";

const ai = new GoogleGenAI({});

export async function transcribePodcastEpisode(audioBase64: string) {
  const result = await ai.models.generateContent({
    model: "gemini-2.5-pro",
    contents: [
      { inlineData: { mimeType: "audio/mpeg", data: audioBase64 } },
      {
        text:
          "Transcribe this episode word for word. Label each speaker as Speaker A, " +
          "Speaker B, and so on. Keep filler words.",
      },
    ],
  });
  return result.text;
}
