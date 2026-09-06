// From harishdeivanayagam/rowfill, src/core/transcribe.ts. The repository's
// own model, whisper-1, is not in the catalog, so the cheapest catalog id
// from the same vendor is substituted; nothing else about the call is
// changed. The audio is fetched by URL into memory and handed over as a
// File, which is how the repository's source indexer feeds it.

import axios from "axios"
import { OpenAI } from "openai"

export async function transcribeAudio(url: string) {

    const openai = new OpenAI({
        apiKey: process.env.OPENAI_API_KEY
    })

    const res = await axios.get(url, { responseType: 'arraybuffer' })

    const transcription = await openai.audio.transcriptions.create({
        model: "gpt-4o-mini",
        file: new File([res.data], 'audio.mp3', { type: 'audio/mp3' })
    })

    return transcription.text

}
