const c = new (require("openai"))();
async function chat(review) {
  return c.chat.completions.create({ model: "gpt-5.1", temperature: 0, max_tokens: 4,
    messages: [{ role: "user", content: "Rate the sentiment of this review as positive, neutral, or negative. Reply with one word only.\n\n" + review }] });
}
