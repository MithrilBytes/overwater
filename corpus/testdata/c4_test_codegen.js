const Anthropic = require("@anthropic-ai/sdk");

const anthropic = new Anthropic();

async function codegenUnitTests(moduleSource) {
  const msg = await anthropic.messages.create({
    model: "claude-opus-5",
    max_tokens: 3000,
    system:
      "Write jest unit tests covering the exported functions of the module. " +
      "Output a single test file, code only.",
    messages: [{ role: "user", content: moduleSource }],
  });
  return msg.content[0].text;
}

module.exports = { codegenUnitTests };
