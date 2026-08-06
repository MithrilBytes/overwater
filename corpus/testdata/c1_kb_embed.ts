import OpenAI from "openai";

const openai = new OpenAI();

export async function embedKbChunks(chunks: string[]) {
  const res = await openai.embeddings.create({
    model: "text-embedding-3-large",
    input: chunks,
    dimensions: 1024,
  });
  return res.data.map((d) => d.embedding);
}
