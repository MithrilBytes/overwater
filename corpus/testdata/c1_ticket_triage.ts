import { Mistral } from "@mistralai/mistralai";

const mistral = new Mistral({ apiKey: process.env.MISTRAL_API_KEY });

const QUEUES = ["billing", "outage", "how_to", "abuse"] as const;

export async function triageTicket(subject: string, body: string) {
  const res = await mistral.chat.complete({
    model: "mistral-large-2411",
    temperature: 0,
    messages: [
      {
        role: "system",
        content: `Assign the ticket to exactly one queue: ${QUEUES.join(", ")}. Reply with the queue name only.`,
      },
      { role: "user", content: `${subject}\n\n${body}` },
    ],
  });
  return res.choices[0].message.content;
}
