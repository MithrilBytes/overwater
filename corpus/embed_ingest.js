const OpenAI = require("openai");

const client = new OpenAI();

async function ingestChunks(chunks) {
  const response = await client.embeddings.create({
    model: "text-embedding-3-small",
    input: chunks,
  });
  return response.data.map((item) => item.embedding);
}

module.exports = { ingestChunks };
