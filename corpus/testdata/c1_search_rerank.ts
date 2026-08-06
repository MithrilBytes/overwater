import OpenAI from "openai";

const client = new OpenAI();

export async function rerankSnippets(query: string, snippets: string[]) {
  const numbered = snippets.map((s, i) => `[${i}] ${s}`).join("\n");
  const res = await client.chat.completions.create({
    model: "gpt-4o-mini",
    temperature: 0,
    messages: [
      {
        role: "system",
        content: "Order the snippets from most to least relevant to the query. Reply with the indices as a JSON array.",
      },
      { role: "user", content: `query: ${query}\n${numbered}` },
    ],
  });
  return res.choices[0].message.content;
}
