// From virattt/dexter, src/memory/embeddings.ts. The model id is the
// repository's own default, taken when the caller passes none. The Gemini
// and Ollama branches of createEmbeddingClient are dropped so one model
// string is left; the batching and timeout wrappers around the call stay.

import { OpenAIEmbeddings } from '@langchain/openai';

const DEFAULT_OPENAI_MODEL = 'text-embedding-3-small';
const EMBEDDING_BATCH_SIZE = 64;
const EMBEDDING_TIMEOUT_MS = 15_000;

export type EmbeddingProviderId = 'auto' | 'openai' | 'none';

export interface MemoryEmbeddingClient {
  provider: 'openai';
  model: string;
  embed: (texts: string[]) => Promise<number[][]>;
}

function withTimeout<T>(promise: Promise<T>, ms: number, message: string): Promise<T> {
  return new Promise<T>((resolve, reject) => {
    const timer = setTimeout(() => reject(new Error(message)), ms);
    promise.then(
      (value) => {
        clearTimeout(timer);
        resolve(value);
      },
      (error) => {
        clearTimeout(timer);
        reject(error);
      },
    );
  });
}

function resolveProvider(preferred: EmbeddingProviderId): 'openai' | null {
  if (preferred === 'openai' && process.env.OPENAI_API_KEY) {
    return 'openai';
  }

  if (preferred === 'auto') {
    if (process.env.OPENAI_API_KEY) {
      return 'openai';
    }
  }

  return null;
}

async function embedInBatches(
  texts: string[],
  embedBatch: (batch: string[]) => Promise<number[][]>,
): Promise<number[][]> {
  const vectors: number[][] = [];
  for (let i = 0; i < texts.length; i += EMBEDDING_BATCH_SIZE) {
    const batch = texts.slice(i, i + EMBEDDING_BATCH_SIZE);
    const result = await withTimeout(embedBatch(batch), EMBEDDING_TIMEOUT_MS, 'Embedding API timed out');
    vectors.push(...result);
  }
  return vectors;
}

export function createEmbeddingClient(params: {
  provider: EmbeddingProviderId;
  model?: string;
}): MemoryEmbeddingClient | null {
  const resolved = resolveProvider(params.provider);
  if (!resolved) {
    return null;
  }

  const model = params.model || DEFAULT_OPENAI_MODEL;
  const embeddings = new OpenAIEmbeddings({
    apiKey: process.env.OPENAI_API_KEY,
    model,
  });
  return {
    provider: 'openai',
    model,
    embed: async (texts: string[]) =>
      embedInBatches(texts, async (batch) => embeddings.embedDocuments(batch)),
  };
}

export async function embedSingleQuery(
  client: MemoryEmbeddingClient | null,
  query: string,
): Promise<number[] | null> {
  if (!client) {
    return null;
  }
  const vectors = await withTimeout(client.embed([query]), EMBEDDING_TIMEOUT_MS, 'Embedding query timed out');
  return vectors[0] ?? null;
}
