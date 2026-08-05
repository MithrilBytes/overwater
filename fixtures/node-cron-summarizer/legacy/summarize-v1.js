const OpenAI = require("openai");

const client = new OpenAI();

async function summarize(text) {
  const response = await client.completions.create({
    model: "text-davinci-003",
    max_tokens: 256,
    prompt: "Summarize this article in three sentences:\n\n" + text,
  });
  return response.choices[0].text;
}

module.exports = { summarize };
