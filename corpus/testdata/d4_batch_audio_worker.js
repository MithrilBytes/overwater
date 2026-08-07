const fs = require("fs");
const OpenAI = require("openai");

const client = new OpenAI();

async function transcribeFile(path) {
  const result = await client.audio.transcriptions.create({
    file: fs.createReadStream(path),
    model: "gpt-4o",
    timestamp_granularities: ["segment"],
    response_format: "verbose_json",
  });
  return result.segments;
}

module.exports = { transcribeFile };
