const OpenAI = require("openai");

const client = new OpenAI();

// Agents read every ticket in English regardless of what came in.
async function toEnglish(body) {
  const response = await client.chat.completions.create({
    model: "gpt-4o-mini",
    temperature: 0,
    max_tokens: 1200,
    messages: [
      { role: "system", content: "Render the customer's message in English. Keep the meaning and tone; do not shorten it." },
      { role: "user", content: body },
    ],
  });
  return response.choices[0].message.content;
}

module.exports = { toEnglish };
