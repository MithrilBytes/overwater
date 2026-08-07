import { VoyageAIClient } from "voyageai";

const voyage = new VoyageAIClient({ apiKey: process.env.VOYAGE_API_KEY });

export async function queryVector(query) {
  const result = await voyage.embed({
    model: "voyage-3.5-lite",
    input: [query],
    inputType: "query",
  });
  return result.data[0].embedding;
}
