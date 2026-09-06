// From harishdeivanayagam/rowfill, src/core/imageToMarkdown.ts. The model
// literal is the repository's own and the call is unchanged, including the
// odd indentation inside the prompt. The image arrives by URL from the
// repository's source indexer.

import { OpenAI } from "openai"

export async function imageToMarkdown(url: string) {

    const openai = new OpenAI({
        apiKey: process.env.OPENAI_API_KEY
    })

    const response = await openai.chat.completions.create({
        model: "gpt-4o-mini",
        messages: [
            {
                role: "system",
                content: "You are a helpful assistant that converts images to markdown."
            },
            {
                role: "user",
                content: [
                    {
                        type: "text",
                        text: `
                            Convert the text in the image to markdown. 
                            If any charts are present, convert them to tables. 
                            For images generate description of the image.    
                        `
                    },
                    {
                        type: "image_url",
                        image_url: {
                            "url": url,
                        },
                    },
                ],
            }
        ]
    })

    return response.choices[0].message.content || ""
}
