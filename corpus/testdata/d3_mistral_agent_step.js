import { Mistral } from "@mistralai/mistralai";

const client = new Mistral({ apiKey: process.env.MISTRAL_API_KEY });

// Runs until the model stops emitting tool calls.
export async function agentStep(scratchpad, tools) {
  return client.chat.complete({
    model: "mistral-large-2411",
    messages: scratchpad,
    tools,
    maxTokens: 2500,
  });
}
