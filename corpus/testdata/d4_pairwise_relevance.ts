import Anthropic from "@anthropic-ai/sdk";

const client = new Anthropic();

export async function scoreRelevance(query: string, docs: string[]) {
  const message = await client.messages.create({
    model: "claude-haiku-4-5",
    max_tokens: 200,
    temperature: 0,
    system:
      "Score how relevant each numbered document is to the query from 0 to 10, then list the document numbers in descending score order.",
    messages: [{ role: "user", content: `query: ${query}\n${docs.map((d, i) => `${i}. ${d}`).join("\n")}` }],
  });
  return message.content[0].text;
}
