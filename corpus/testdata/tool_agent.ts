import Anthropic from "@anthropic-ai/sdk";

const client = new Anthropic();

const lookupOrder = {
  name: "lookup_order",
  description: "Fetch an order by id.",
  input_schema: {
    type: "object",
    properties: { order_id: { type: "string" } },
    required: ["order_id"],
  },
};

export async function runTurn(history: unknown[]) {
  return client.messages.create({
    model: "claude-sonnet-5",
    max_tokens: 1024,
    tools: [lookupOrder],
    messages: history,
  });
}
