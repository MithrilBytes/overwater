const OpenAI = require("openai");

const client = new OpenAI();

async function describeScreenshot(url) {
  const response = await client.chat.completions.create({
    model: "gpt-4.1",
    max_tokens: 500,
    messages: [
      {
        role: "user",
        content: [
          { type: "text", text: "What is broken in this screenshot? Name the element and the error text shown." },
          { type: "image_url", image_url: { url, detail: "high" } },
        ],
      },
    ],
  });
  return response.choices[0].message.content;
}

module.exports = { describeScreenshot };
