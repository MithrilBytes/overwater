// From DaviMoreira27/instagram-saved-posts-categorizer, src/categorizer.ts,
// with the default strategy from src/llm-strategy.ts, the LLM_MODEL default
// from src/config.ts and the category table from src/types.ts, which is
// where the repository keeps them. The repository can also route the same
// messages through a fallback chain of four providers; that branch and the
// other three strategies are dropped so one model string is left. The
// prompt is Portuguese in the original and is kept that way.

import { BaseLanguageModel } from '@langchain/core/language_models/base';
import { HumanMessage } from '@langchain/core/messages';
import { ChatGoogleGenerativeAI } from '@langchain/google-genai';

export const CATEGORIES = {
  FILMES: 'FILMES',
  SERIES: 'SERIES',
  ANIMES: 'ANIMES',
  CULINARIA: 'CULINARIA',
  MODA: 'MODA',
  ENGRACADOS: 'ENGRACADOS',
  EDUCACIONAL: 'EDUCACIONAL',
  OUTROS: 'OUTROS',
} as const;

export type Category = typeof CATEGORIES[keyof typeof CATEGORIES];

export const CATEGORY_LIST = Object.values(CATEGORIES) as Category[];

export function isValidCategory(category: string): category is Category {
  return CATEGORY_LIST.includes(category as Category);
}

export interface PostData {
  author: string;
  description: string;
  postUrl: string;
  imgUrl: string | undefined;
}

export const CONFIG = {
  batchSize: 5,
  llmModel: (process.env.LLM_MODEL as 'openai' | 'deepseek' | 'qwen' | 'google' | undefined) || 'google',
} as const;

export type LLMModel = 'openai' | 'deepseek' | 'qwen' | 'google';

export interface LLMStrategy {
  invoke: () => Promise<BaseLanguageModel>;
}

class GoogleAIStrategy implements LLMStrategy {
  async invoke(): Promise<BaseLanguageModel> {
    return new ChatGoogleGenerativeAI({
      modelName: 'gemini-1.5-flash',
      apiKey: process.env.GOOGLE_API_KEY,
      temperature: 0.7,
    });
  }
}

class LLMStrategyFactory {
  private strategies: Map<LLMModel, LLMStrategy> = new Map([
    ['google', new GoogleAIStrategy()],
  ]);

  getStrategy(model: LLMModel): LLMStrategy {
    const strategy = this.strategies.get(model);
    if (!strategy) {
      throw new Error(`Estratégia não encontrada para modelo: ${model}`);
    }
    return strategy;
  }
}

let llmInstance: BaseLanguageModel | null = null;

export async function getLLMModel(model: LLMModel = 'google'): Promise<BaseLanguageModel> {
  if (llmInstance) {
    return llmInstance;
  }

  const factory = new LLMStrategyFactory();
  const strategy = factory.getStrategy(model);
  llmInstance = await strategy.invoke();
  return llmInstance;
}

export interface CategorizedPost {
  postUrl: string;
  description: string;
  date: string;
  categoria: string;
}

async function invokeModel(messages: HumanMessage[]): Promise<string> {
  const llm = await getLLMModel(CONFIG.llmModel);
  const response = await llm.invoke(messages);
  return typeof response.content === 'string'
    ? response.content
    : String(response.content);
}

export async function categorizePosts(posts: PostData[]): Promise<(CategorizedPost | null)[]> {
  const postsData = posts
    .map((post, index) => `Post ${index + 1}:\nURL: ${post.postUrl}\nDescrição: ${post.description}`)
    .join('\n\n');

  try {
    const messages = [
      new HumanMessage({
        content: [
          {
            type: 'text',
            text: `Analise estes ${posts.length} posts do Instagram e categorize cada um. Retorne um JSON válido com um array de objetos, cada um contendo:
- postUrl: URL do post
- description: Uma descrição curta e concisa (máximo 150 caracteres)
- date: Data atual no formato YYYY-MM-DD
- categoria: Categoria principal. DEVE ser uma destas: ${CATEGORY_LIST.join(', ')}

Posts para categorizar:
${postsData}

Retorne APENAS um array JSON, sem markdown ou explicações adicionais. Exemplo: [{ "postUrl": "...", "description": "...", "date": "...", "categoria": "..." }, ...]`,
          },
          ...posts
            .filter(p => p.imgUrl)
            .map(post => ({
              type: 'image_url' as const,
              image_url: {
                url: post.imgUrl!,
              },
            })),
        ],
      }),
    ];

    const responseText = await invokeModel(messages);

    const jsonMatch = responseText.match(/\[[\s\S]*\]/);
    if (!jsonMatch) {
      throw new Error('Resposta da IA não contém array JSON válido');
    }

    const results: CategorizedPost[] = JSON.parse(jsonMatch[0]);

    if (!Array.isArray(results) || results.length === 0) {
      throw new Error('Resposta da IA não é um array válido');
    }

    const categorizedResults: (CategorizedPost | null)[] = [];

    for (let i = 0; i < posts.length; i++) {
      const post = posts[i];
      const result = results[i];

      if (!result || !result.postUrl) {
        console.error(`Resultado inválido para post ${i + 1}`);
        await enqueuePost(post);
        categorizedResults.push(null);
        continue;
      }

      if (!isValidCategory(result.categoria)) {
        console.warn(`Categoria inválida recebida: ${result.categoria}. Usando OUTROS como padrão.`);
        result.categoria = 'OUTROS';
      }

      await saveAndMarkCategorized(result, post);
      console.log(`✓ Post categorizado: ${result.postUrl} - ${result.categoria}`);
      categorizedResults.push(result);
    }

    return categorizedResults;
  } catch (error) {
    const message = error instanceof Error ? error.message : String(error);
    console.error(`✗ Erro ao categorizar batch de ${posts.length} posts: ${message}`);
    for (const post of posts) {
      await enqueuePost(post);
    }
    return posts.map(() => null);
  }
}

declare function enqueuePost(post: PostData): Promise<void>;
declare function saveAndMarkCategorized(result: CategorizedPost, post: PostData): Promise<void>;
