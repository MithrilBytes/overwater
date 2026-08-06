import { Mistral } from "@mistralai/mistralai";

const client = new Mistral({ apiKey: process.env.CODESTRAL_API_KEY });

// Inline completion endpoint backing the editor plugin.
export async function codegenInlineCompletion(prefix: string, suffix: string) {
  const completion = await client.fim.complete({
    model: "codestral-2501",
    prompt: prefix,
    suffix: suffix,
    maxTokens: 128,
    temperature: 0.1,
    stop: ["\n\n"],
  });
  return completion.choices?.[0]?.message?.content ?? "";
}
