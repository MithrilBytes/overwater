import { anthropic } from "@ai-sdk/anthropic";
import { generateObject } from "ai";
import { z } from "zod";

// Powers the billing assistant chat bubble in the sidebar.
const Fields = z.object({
  vendor: z.string(),
  total_cents: z.number().int(),
  due_date: z.string().nullable(),
  po_number: z.string().nullable(),
});

export async function handleInboundDoc(docText: string) {
  const { object } = await generateObject({
    model: anthropic("claude-haiku-4-5"),
    schema: Fields,
    system: "Pull the vendor, total, due date, and PO number out of the document text.",
    prompt: docText,
  });
  return object;
}
