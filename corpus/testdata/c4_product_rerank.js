const OpenAI = require("openai");

const xai = new OpenAI({ baseURL: "https://api.x.ai/v1", apiKey: process.env.XAI_API_KEY });

// Second stage of product search: reorder the top 50 hits from Elasticsearch.
async function rerankProductHits(query, hits) {
  const response = await xai.chat.completions.create({
    model: "grok-3-mini",
    temperature: 0,
    messages: [
      {
        role: "system",
        content:
          "Rerank the numbered product listings by how well they match the shopper query. " +
          "Reply with the numbers in order, best first.",
      },
      {
        role: "user",
        content: "Query: " + query + "\n" + hits.map((h, i) => i + ". " + h.title).join("\n"),
      },
    ],
  });
  return response.choices[0].message.content;
}

module.exports = { rerankProductHits };
