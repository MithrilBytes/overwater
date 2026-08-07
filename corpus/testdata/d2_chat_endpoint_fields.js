const OpenAI = require("openai");

const client = new OpenAI();

// Runs on the support chat transcript queue.
async function chatFieldSweep(transcript) {
  const response = await client.chat.completions.create({
    model: "gpt-4o-mini",
    temperature: 0,
    max_tokens: 300,
    response_format: {
      type: "json_schema",
      json_schema: {
        name: "case_fields",
        schema: {
          type: "object",
          properties: {
            account_id: { type: "string" },
            product: { type: "string" },
            error_code: { type: "string" },
            requested_refund: { type: "number" },
          },
        },
      },
    },
    messages: [
      { role: "system", content: "Read out the case fields the agent confirmed. Do not guess a value that was never said." },
      { role: "user", content: transcript },
    ],
  });
  return JSON.parse(response.choices[0].message.content);
}

module.exports = { chatFieldSweep };
