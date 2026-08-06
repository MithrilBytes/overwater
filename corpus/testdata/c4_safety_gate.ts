import Anthropic from "@anthropic-ai/sdk";

const anthropic = new Anthropic();

export async function moderateIncomingMessage(text: string): Promise<boolean> {
  const msg = await anthropic.messages.create({
    model: "claude-haiku-4-5",
    max_tokens: 5,
    system:
      "You are a strict content safety filter for a kids homework app. " +
      "Reply with exactly ALLOW or BLOCK.",
    messages: [{ role: "user", content: text }],
  });
  const verdict = msg.content[0].type === "text" ? msg.content[0].text : "BLOCK";
  return verdict.trim() === "ALLOW";
}
