const OpenAI = require("openai");

const client = new OpenAI();

async function dailyDigest(articles) {
  const response = await client.completions.create({
    model: "text-davinci-003",
    max_tokens: 400,
    prompt: "Summarize these articles for the morning digest:\n\n" + articles.join("\n"),
  });
  return response.choices[0].text;
}

module.exports = { dailyDigest };
