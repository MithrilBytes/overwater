const OpenAI = require("openai");

const client = new OpenAI();

// Conversational reply endpoint for the embedded help widget.
async function widgetChatReply(query, passages) {
  const numbered = passages.map((p, i) => i + ". " + p).join("\n");
  const response = await client.chat.completions.create({
    model: "gpt-5-nano",
    temperature: 0,
    messages: [
      {
        role: "system",
        content:
          "You rerank candidate passages. Return the passage numbers ordered from " +
          "most to least relevant to the query. Numbers only, no reply text.",
      },
      { role: "user", content: "Query: " + query + "\n" + numbered },
    ],
  });
  return response.choices[0].message.content;
}

module.exports = { widgetChatReply };
