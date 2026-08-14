// From biztex/AI_vita, backend/src/services/axelWebSearch.ts, with the env
// default it reads from backend/src/env.ts. The repository's own default,
// gpt-5.6-sol, is not in the catalog, so a catalog id is substituted; the
// call is otherwise as written. The instruction is Japanese in the original
// and is kept that way.

import OpenAI from 'openai';

const ENV = {
  // Model for the web-search research pass (must support the web_search tool).
  AXEL_SEARCH_MODEL: process.env.AXEL_SEARCH_MODEL || 'gpt-5.1',
};

const client = new OpenAI({ apiKey: process.env.OPENAI_API_KEY });

const NO_SEARCH = 'NO_SEARCH';

const RESEARCH_INSTRUCTION = [
  'あなたはAXELというコンシェルジュのためのリサーチ補助です。',
  'ユーザーの相談に、最新の状況や外部の固有事実（開催・募集・価格・制度・営業状況・評判・仕様など、時間や調査で確定する情報）が必要な場合のみ、Web検索を行ってください。',
  '検索する場合は、公式サイト・公的機関・一次情報など信頼性の高い情報源を優先し、日本語で要点だけを簡潔にまとめてください。',
  '公式ドメインで確認できた事実と、非公式情報・推測・一般論は必ず区別し、確認できないことは「確認できない」と明示してください。',
  '末尾に、参照した情報源を「・（名称） （URL）」形式で列挙してください。公式ドメインを先に挙げてください。',
  'マークダウン記法（#、**、---、表）は使わないでください。',
  `外部の事実確認が不要な相談（雑談・気持ち・一般常識・計算・すでに分かっている前提の相談）の場合は、検索せず「${NO_SEARCH}」だけを返してください。`,
].join('\n');

export async function researchIfNeeded(userText: string): Promise<string | null> {
  try {
    const resp = await client.responses.create({
      model: ENV.AXEL_SEARCH_MODEL,
      tools: [{ type: 'web_search' as any }],
      instructions: RESEARCH_INSTRUCTION,
      input: userText.slice(0, 1500),
      max_output_tokens: 1800,
      reasoning: { effort: 'low' },
    } as any);

    let searched = false;
    let text = '';
    for (const o of (resp as any)?.output ?? []) {
      if (o?.type === 'web_search_call') searched = true;
      if (o?.type === 'message') {
        for (const c of o?.content ?? []) {
          if (c?.type === 'output_text') text += c.text ?? '';
        }
      }
    }
    if (!searched || !text || text.startsWith(NO_SEARCH)) return null;
    return text.trim();
  } catch (err: any) {
    console.error('[axelWebSearch] search failed (non-fatal):', err?.code || err?.message || err);
    return null;
  }
}
