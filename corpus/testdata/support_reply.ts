import Anthropic from "@anthropic-ai/sdk";

const client = new Anthropic();

export async function replyToCustomer(thread: string) {
  return client.messages.stream({
    model: "claude-sonnet-5",
    max_tokens: 1500,
    system: "You are a support assistant. Answer from the thread, ask when unsure.",
    messages: [{ role: "user", content: thread }],
  });
}
