const OpenAI = require("openai");

const client = new OpenAI();

// Last check before the draft leaves our network.
async function leaksPii(draft) {
  const response = await client.chat.completions.create({
    model: "gpt-4.1-nano",
    temperature: 0,
    max_tokens: 3,
    messages: [
      { role: "system", content: "Does this text contain personal data that policy forbids sending outside the company? Answer true or false." },
      { role: "user", content: draft },
    ],
  });
  return response.choices[0].message.content.trim() === "true";
}

module.exports = { leaksPii };
