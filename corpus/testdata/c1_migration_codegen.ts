import OpenAI from "openai";

const client = new OpenAI();

export async function codegenMigration(tableSpec: string) {
  const res = await client.responses.create({
    model: "gpt-5-mini",
    instructions: "Write a Postgres migration in SQL for the requested schema change. Output only SQL.",
    input: tableSpec,
  });
  return res.output_text;
}
