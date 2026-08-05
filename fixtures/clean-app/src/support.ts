import Anthropic from "@anthropic-ai/sdk";

const client = new Anthropic();

const TRIAGE_RULES = `You triage support tickets for Overwater Labs. Assign one of: billing, bug, question, feedback. Assign one priority: p1 for production impact with no workaround, p2 for degraded service or a blocked paying customer, p3 for everything else. Billing disputes go to billing even when the customer also reports a bug. Security reports are always p1. Never reply to the customer and never promise refunds, credits, or timelines. When a ticket mixes several requests, triage the first request and note the rest in one sentence. When you are unsure between two priorities, choose the lower urgency and say why in one sentence. Tickets can arrive in any language; triage on meaning rather than keywords. Quote the exact error message in your notes when one is present, because the support team searches by error text. Mark a ticket as feedback only when the customer asks for nothing. If the ticket reports a fake login page, malware, or spam hosted on our platform, set priority p1 and add the word abuse to your notes so the trust team gets paged.`;

export async function triageTicket(ticket: string) {
  const message = await client.messages.create({
    model: "claude-haiku-4-5",
    max_tokens: 100,
    temperature: 0,
    system: [
      {
        type: "text",
        text: TRIAGE_RULES,
        cache_control: { type: "ephemeral" },
      },
    ],
    messages: [{ role: "user", content: ticket }],
  });
  return message.content[0];
}

export async function draftReply(ticket: string, notes: string) {
  return client.messages.stream({
    model: "claude-sonnet-5",
    max_tokens: 2000,
    system: [
      {
        type: "text",
        text: TRIAGE_RULES,
        cache_control: { type: "ephemeral" },
      },
    ],
    messages: [
      { role: "user", content: "Ticket: " + ticket + "\n\nTriage notes: " + notes },
    ],
  });
}
