const OpenAI = require("openai");

const client = new OpenAI();

async function tldr(thread) {
  const response = await client.chat.completions.create({
    model: "gpt-4o-mini",
    max_tokens: 300,
    messages: [
      { role: "system", content: "Give the TL;DR of this Slack thread in two sentences, then list any open question." },
      { role: "user", content: thread.join("\n") },
    ],
  });
  return response.choices[0].message.content;
}

module.exports = { tldr };
