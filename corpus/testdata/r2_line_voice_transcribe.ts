// From biztex/AI_vita, backend/src/services/axelVoice.ts, with the env
// default it reads from backend/src/env.ts. The repository's own default,
// gpt-4o-transcribe, is not in the catalog, so the nearest catalog id is
// substituted; nothing else about the call is changed.

import OpenAI, { toFile } from 'openai';

const ENV = {
  // Speech-to-text model for LINE voice messages (client spec 3).
  AXEL_TRANSCRIBE_MODEL: process.env.AXEL_TRANSCRIBE_MODEL || 'gpt-4o',
};

const client = new OpenAI({ apiKey: process.env.OPENAI_API_KEY });

// Silence/noise makes transcription models hallucinate a short phrase in a
// random language with LOW token confidence (real speech scores ~-0.0 avg
// logprob; silence ~-2.1). Gate on average logprob so noise is treated as
// unheard regardless of what language it hallucinated.
const MIN_AVG_LOGPROB = -1.2;

export async function transcribeAudio(
  audio: Buffer,
  filename = 'audio.m4a',
): Promise<string | null> {
  try {
    const file = await toFile(audio, filename);
    const res: any = await client.audio.transcriptions.create({
      file,
      model: ENV.AXEL_TRANSCRIBE_MODEL,
      language: 'ja',
      response_format: 'json',
      include: ['logprobs'],
    } as any);
    const text = (res?.text ?? '').trim();
    if (text.length === 0) return null;

    const lps: any[] = Array.isArray(res?.logprobs) ? res.logprobs : [];
    if (lps.length > 0) {
      const avg = lps.reduce((s, t) => s + (t?.logprob ?? 0), 0) / lps.length;
      if (avg < MIN_AVG_LOGPROB) {
        return null;
      }
    }
    return text;
  } catch (err: any) {
    console.error('[axelVoice] transcription failed (non-fatal):', err?.code || err?.message || err);
    return null;
  }
}
