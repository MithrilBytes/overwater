const OpenAI = require("openai");

const client = new OpenAI();

// The widget shows the top three of whatever comes back.
async function pickArticles(question, articles) {
  const response = await client.chat.completions.create({
    model: "gpt-5-nano",
    temperature: 0,
    max_tokens: 60,
    messages: [
      { role: "system", content: "Sort the help articles by relevance to the question, most relevant first. Reply with the slugs in order." },
      { role: "user", content: question + "\n" + articles.join("\n") },
    ],
  });
  return response.choices[0].message.content;
}

module.exports = { pickArticles };
