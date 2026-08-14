// From biztex/AI_vita, backend/src/services/axelUnderstanding.ts, with the
// env default it reads from backend/src/env.ts. The repository's own
// default, gpt-5.6-luna, is not in the catalog, so a catalog id is
// substituted. The prompt is Japanese in the original and is kept that way;
// the caps stay behind tuningParams(), as they are in the repository.

import OpenAI from 'openai';

const ENV = {
  AXEL_UPDATER_MODEL: process.env.AXEL_UPDATER_MODEL || 'gpt-5-mini',
};

const openai = new OpenAI({ apiKey: process.env.OPENAI_API_KEY });

function buildUpdaterPrompt(onboarding: unknown, prefs: unknown, notes: string | null): string {
  return [
    'あなたは、AIコンシェルジュ「AXEL」の理解記録係です。',
    '直近の一往復の会話を読み、AXELがこの利用者について新しく理解できたことだけを、JSONで返してください。',
    '',
    '【現在の理解（既知の情報は返さないこと）】',
    JSON.stringify({ profile: onboarding ?? {}, personaPrefs: prefs ?? {}, notes: notes ?? '' }, null, 0),
    '',
    '【返すJSONの形（全フィールド任意。新情報がなければ空オブジェクト {} を返す）】',
    '{',
    '  "profile": { "name":"呼び名", "stage":"事業フェーズ", "values":"価値観", "decisionTheme":"今の判断テーマ",',
    '    "healthGoals":["健康目標"], "thinkingStyle":"戦略型/分析型/直感型/共感型", "personality":"性格特性",',
    '    "background":"経歴・実績", "currentBusiness":"現在の事業", "futureGoals":"将来目標" },',
    '  "personaPrefs": { "addressAs":"希望の呼ばれ方", "tone":"希望の口調・距離感", "detail":"回答の詳しさの希望" },',
    '  "noteAppend": "どの項目にも収まらない、1文の短い観察（なければ null）",',
    '  "decisionChosen": "決めたと報告された内容" または null',
    '}',
    '',
    '【厳守ルール】',
    '・利用者が実際に言ったこと、明確に肯定したことだけを抽出する。推測を事実にしない。',
    '・挨拶・雑談・お礼・短い相づちだけの往復なら、memory は null、他も空にする。',
    '・値は短く（各40文字以内、gistのみ160文字以内）。',
  ].join('\n');
}

export async function runUnderstandingUpdate(userText: string, axelReply: string): Promise<void> {
  const completion = await openai.chat.completions.create({
    model: ENV.AXEL_UPDATER_MODEL,
    messages: [
      { role: 'system', content: buildUpdaterPrompt(null, null, null) },
      { role: 'user', content: `利用者：「${userText.slice(0, 600)}」\nAXEL：「${axelReply.slice(0, 600)}」` },
    ],
    response_format: { type: 'json_object' },
    // temperature 0 applies only to legacy models; reasoning-family models
    // run at their fixed default and rely on the prompt for discipline.
    ...(tuningParams(ENV.AXEL_UPDATER_MODEL, { maxTokens: 500, temperature: 0 }) as any),
  });
  parseUnderstandingJson(completion.choices[0]?.message?.content ?? '{}');
}

declare function tuningParams(model: string, opts: Record<string, number>): Record<string, unknown>;
declare function parseUnderstandingJson(raw: string): void;
