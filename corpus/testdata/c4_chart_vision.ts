import OpenAI from "openai";

const client = new OpenAI();

export async function readChartWithVision(imageUrl: string) {
  const response = await client.chat.completions.create({
    model: "gpt-5.1",
    messages: [
      {
        role: "user",
        content: [
          {
            type: "text",
            text: "Read this chart. Report the axis labels and the value of every series at each x point as JSON.",
          },
          { type: "image_url", image_url: { url: imageUrl } },
        ],
      },
    ],
  });
  return response.choices[0].message.content;
}
