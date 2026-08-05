const cron = require("node-cron");
const OpenAI = require("openai");

const { fetchOvernightArticles, postToSlack } = require("./sources");

const client = new OpenAI();

const DIGEST_PROMPT = `You write the Acme engineering morning digest. Summarize the articles you are given in plain language for busy engineers. Lead with the single most consequential item. Group related items into one entry instead of repeating them. Keep each entry to two sentences: what happened, and why an engineer at a hosting company should care. Skip funding news unless the company is a direct competitor. Skip opinion pieces unless they contain original benchmarks. Write in full sentences, never bullet points, and never invent a detail that is not in the source text.`;

async function summarizeArticles(articles) {
  const response = await client.chat.completions.create({
    model: "gpt-5.1",
    max_tokens: 800,
    messages: [
      { role: "system", content: DIGEST_PROMPT },
      { role: "user", content: articles.join("\n\n") },
    ],
  });
  return response.choices[0].message.content;
}

cron.schedule("0 6 * * *", async () => {
  const articles = await fetchOvernightArticles();
  const digest = await summarizeArticles(articles);
  await postToSlack(digest);
});

module.exports = { summarizeArticles };
