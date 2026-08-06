const { Mistral } = require("@mistralai/mistralai");

const client = new Mistral({ apiKey: process.env.MISTRAL_API_KEY });

async function translateHelpArticle(markdown, targetLang) {
  const response = await client.chat.complete({
    model: "mistral-small-2506",
    messages: [
      {
        role: "system",
        content:
          "You translate help center articles. Preserve markdown structure, code blocks, " +
          "and links. Translate the prose into " + targetLang + ".",
      },
      { role: "user", content: markdown },
    ],
  });
  return response.choices[0].message.content;
}

module.exports = { translateHelpArticle };
