import Anthropic from "@anthropic-ai/sdk";

const anthropic = new Anthropic();

export async function extractInvoiceFields(rawText: string) {
  const msg = await anthropic.messages.create({
    model: "claude-haiku-4-5",
    max_tokens: 400,
    tools: [{
      name: "record_invoice",
      description: "Record the fields found on an invoice.",
      input_schema: {
        type: "object",
        properties: {
          vendor: { type: "string" },
          invoice_number: { type: "string" },
          total_cents: { type: "integer" },
          due_date: { type: "string" },
        },
        required: ["vendor", "total_cents"],
      },
    }],
    tool_choice: { type: "tool", name: "record_invoice" },
    messages: [{ role: "user", content: rawText }],
  });
  const block = msg.content.find((b) => b.type === "tool_use");
  return block && "input" in block ? block.input : null;
}
