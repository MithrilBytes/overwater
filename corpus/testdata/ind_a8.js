const c = new (require("openai"))();
async function gate(comment) {
  return c.chat.completions.create({ model: "gpt-5-mini", temperature: 0, max_tokens: 3,
    messages: [{ role: "user", content: "Does this comment violate our harassment policy? Answer yes or no.\n\n" + comment }] });
}
