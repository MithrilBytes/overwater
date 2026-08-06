import Anthropic from "@anthropic-ai/sdk";

const anthropic = new Anthropic();

export async function translateHandoffNote(thread: string) {
  const msg = await anthropic.messages.create({
    model: "claude-sonnet-5",
    max_tokens: 300,
    system:
      "Summarize the support conversation into three short bullet points for the " +
      "next agent. Keep the customer's own language, do not change it.",
    messages: [{ role: "user", content: thread }],
  });
  return msg.content[0].type === "text" ? msg.content[0].text : "";
}
