import Anthropic from "@anthropic-ai/sdk";

const client = new Anthropic();

const RECORD = {
  name: "record_lead",
  description: "Store the lead fields read off the inbound email.",
  input_schema: {
    type: "object" as const,
    properties: {
      company: { type: "string" },
      contact_name: { type: "string" },
      budget: { type: "number" },
      timeline: { type: "string" },
    },
  },
};

// One shot: the tool is forced, so there is no second turn.
export async function readLead(email: string) {
  const message = await client.messages.create({
    model: "claude-haiku-4-5",
    max_tokens: 400,
    temperature: 0,
    tools: [RECORD],
    tool_choice: { type: "tool", name: "record_lead" },
    messages: [{ role: "user", content: email }],
  });
  return message.content[0];
}
