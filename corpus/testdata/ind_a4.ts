import Anthropic from "@anthropic-ai/sdk";
const c = new Anthropic();
export async function nightly(prs: string[]) {
  return c.messages.create({ model: "claude-sonnet-5", max_tokens: 1200,
    system: "Condense this week's merged pull requests into a short changelog for the team newsletter.",
    messages: [{ role: "user", content: prs.join("\n") }] });
}
