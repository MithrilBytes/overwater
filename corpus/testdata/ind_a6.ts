import Anthropic from "@anthropic-ai/sdk";
const c = new Anthropic();
const tools = [{ name: "search_orders", input_schema: { type: "object" } },
               { name: "issue_refund", input_schema: { type: "object" } },
               { name: "escalate", input_schema: { type: "object" } }];
export async function step(history: unknown[]) {
  return c.messages.create({ model: "claude-opus-5", max_tokens: 2000, tools, messages: history });
}
