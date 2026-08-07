import OpenAI from "openai";

const client = new OpenAI();

const tools = [
  { type: "function" as const, name: "search_orders", description: "Find orders by email.", parameters: { type: "object", properties: { email: { type: "string" } } } },
  { type: "function" as const, name: "issue_refund", description: "Refund an order.", parameters: { type: "object", properties: { orderId: { type: "string" } } } },
];

export async function nextStep(previousResponseId: string, input: unknown[]) {
  return client.responses.create({
    model: "gpt-5.1",
    tools,
    input,
    previous_response_id: previousResponseId,
    max_output_tokens: 2000,
  });
}
