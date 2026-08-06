const { VoyageAIClient } = require("voyageai");

const voyage = new VoyageAIClient({ apiKey: process.env.VOYAGE_API_KEY });

async function embedForIndex(passages) {
  const result = await voyage.embed({
    input: passages,
    model: "voyage-3.5",
    inputType: "document",
  });
  return result.data.map((row) => row.embedding);
}

module.exports = { embedForIndex };
