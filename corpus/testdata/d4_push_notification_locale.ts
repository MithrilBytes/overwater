import Anthropic from "@anthropic-ai/sdk";

const client = new Anthropic();

export async function localizePush(copy: string, locale: string) {
  const message = await client.messages.create({
    model: "claude-haiku-4-5",
    max_tokens: 150,
    temperature: 0,
    system: "Translate the push notification into the target locale. Stay under 120 characters and keep the {name} token.",
    messages: [{ role: "user", content: `${locale}\n${copy}` }],
  });
  return message.content[0].text;
}
