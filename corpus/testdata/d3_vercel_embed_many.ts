import { openai } from "@ai-sdk/openai";
import { embedMany } from "ai";

export async function embedChunks(chunks: string[]) {
  const { embeddings } = await embedMany({
    model: openai.embedding("text-embedding-3-small"),
    values: chunks,
  });
  return embeddings;
}
