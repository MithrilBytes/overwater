const Anthropic = require("@anthropic-ai/sdk");

const client = new Anthropic({ apiKey: process.env.ANTHROPIC_API_KEY });

async function buildReleaseDigest(commitLog) {
  const message = await client.messages.create({
    model: "claude-opus-5",
    max_tokens: 700,
    system: "Turn the commit log into a short release digest for the changelog page.",
    messages: [{ role: "user", content: commitLog }],
  });
  return message.content[0].text;
}

module.exports = { buildReleaseDigest };
