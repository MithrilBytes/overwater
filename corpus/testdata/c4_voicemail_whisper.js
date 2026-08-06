const fs = require("fs");
const OpenAI = require("openai");

const client = new OpenAI();

// Voicemail worker: pulls new recordings off the queue, whisper style.
async function transcribeVoicemail(recordingPath) {
  const transcript = await client.audio.transcriptions.create({
    file: fs.createReadStream(recordingPath),
    model: "gpt-5-mini",
    language: "en",
  });
  return transcript.text;
}

module.exports = { transcribeVoicemail };
