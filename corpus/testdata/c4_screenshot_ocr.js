const { GoogleGenAI } = require("@google/genai");

const ai = new GoogleGenAI({});

// Support tooling: pull the visible text out of user submitted screenshots.
async function ocrScreenshot(pngBase64) {
  const result = await ai.models.generateContent({
    model: "gemini-2.5-flash",
    contents: [
      { inlineData: { mimeType: "image/png", data: pngBase64 } },
      {
        text: "Run OCR on this screenshot. Return every piece of visible text, one line per UI element.",
      },
    ],
  });
  return result.text;
}

module.exports = { ocrScreenshot };
