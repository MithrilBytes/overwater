import OpenAI from "openai";

const client = new OpenAI();

export async function ocrShippingLabel(imageUrl: string) {
  const res = await client.chat.completions.create({
    model: "gpt-4o",
    messages: [
      {
        role: "user",
        content: [
          { type: "text", text: "Read the tracking number and carrier printed on this shipping label." },
          { type: "image_url", image_url: { url: imageUrl } },
        ],
      },
    ],
  });
  return res.choices[0].message.content;
}
