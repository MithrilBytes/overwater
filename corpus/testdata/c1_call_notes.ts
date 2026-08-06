import { CohereClientV2 } from "cohere-ai";

const cohere = new CohereClientV2({ token: process.env.COHERE_API_KEY });

export async function summarizeCallNotes(notes: string) {
  const reply = await cohere.chat({
    model: "command-a-03-2025",
    messages: [
      { role: "system", content: "Condense the sales call notes into a five line summary." },
      { role: "user", content: notes },
    ],
  });
  return reply.message?.content?.[0]?.text;
}
