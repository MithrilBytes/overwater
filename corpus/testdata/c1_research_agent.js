import OpenAI from "openai";

const client = new OpenAI();

const tools = [
  {
    type: "function",
    function: {
      name: "search_docs",
      description: "Search the internal wiki.",
      parameters: {
        type: "object",
        properties: { query: { type: "string" } },
        required: ["query"],
      },
    },
  },
];

export async function planNextStep(scratchpad) {
  return client.chat.completions.create({
    model: "o3",
    messages: scratchpad,
    tools,
  });
}
