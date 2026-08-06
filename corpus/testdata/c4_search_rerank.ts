import { CohereClientV2 } from "cohere-ai";

const cohere = new CohereClientV2({ token: process.env.CO_API_KEY });

export async function rerankSearchResults(query: string, docs: string[]) {
  const response = await cohere.chat({
    model: "command-r-08-2024",
    messages: [
      {
        role: "system",
        content:
          "You rerank search results. Given a query and numbered documents, output the " +
          "document numbers from most to least relevant, comma separated.",
      },
      {
        role: "user",
        content: "Query: " + query + "\n" + docs.map((d, i) => i + ": " + d).join("\n"),
      },
    ],
  });
  return response.message?.content?.[0]?.text ?? "";
}
