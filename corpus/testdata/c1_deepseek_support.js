const OpenAI = require("openai");

const client = new OpenAI({
  baseURL: "https://api.deepseek.com",
  apiKey: process.env.DEEPSEEK_API_KEY,
});

async function handleChatTurn(history, userText) {
  const stream = await client.chat.completions.create({
    model: "deepseek-chat",
    messages: [...history, { role: "user", content: userText }],
    stream: true,
  });
  return stream;
}

module.exports = { handleChatTurn };
