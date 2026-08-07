const OpenAI = require("openai");

const client = new OpenAI();

async function writeFilter(sample, ask) {
  const response = await client.chat.completions.create({
    model: "gpt-4.1-mini",
    max_tokens: 300,
    temperature: 0,
    messages: [
      { role: "system", content: "Write one jq filter that does what the user asks against the sample JSON. Output the filter only." },
      { role: "user", content: sample + "\n" + ask },
    ],
  });
  return response.choices[0].message.content;
}

module.exports = { writeFilter };
