const OpenAI = require("openai");

const client = new OpenAI();

// The speech to text step already ran; this is the cleanup pass.
async function condenseTranscript(rawTranscript) {
  const response = await client.chat.completions.create({
    model: "gpt-4.1-mini",
    max_tokens: 700,
    messages: [
      {
        role: "system",
        content:
          "Turn the raw transcript into a short readable summary: three paragraphs, no timestamps, no speaker labels, keep only what matters.",
      },
      { role: "user", content: rawTranscript },
    ],
  });
  return response.choices[0].message.content;
}

module.exports = { condenseTranscript };
