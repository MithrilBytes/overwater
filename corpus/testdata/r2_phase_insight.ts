// From biztex/AI_vita, backend/src/routes/diagnostic.ts. The model literal
// is the repository's own. The prompt is Japanese in the original and is
// kept that way: a one or two sentence casual reaction to the user's
// personality-test result, sent back to them mid-quiz.

import { Router } from 'express';
import OpenAI from 'openai';

const openai = new OpenAI({ apiKey: process.env.OPENAI_API_KEY });
export const router = Router();

router.post('/phase-insight', async (req, res) => {
  const { phase, mbtiType, discType } = req.body ?? {};

  let insightPrompt = '';
  switch (phase) {
    case 'MBTI': {
      const type = mbtiType || '';
      const isExtrovert = type.startsWith('E');
      insightPrompt = `MBTIタイプ ${type} の結果に基づいて、ユーザーへのカジュアルで短い（1〜2文）フィードバックを日本語で生成してください。「キミって${isExtrovert ? '外向' : '内向'}タイプっぽいですか？」のようなフレンドリーなトーンで。`;
      break;
    }
    case 'DISC': {
      const dType = discType || '';
      insightPrompt = `DISCタイプ ${dType} の結果に基づいて、ユーザーへのカジュアルで短い（1〜2文）フィードバックを日本語で生成してください。「${dType === 'D' ? '結構リーダーシップ強めですね！' : 'チーム作りが得意そうですね！'}」のようなフレンドリーなトーンで。`;
      break;
    }
    default:
      return res.status(400).json({ error: 'Invalid phase' });
  }

  const completion = await openai.chat.completions.create({
    model: 'gpt-4o-mini',
    max_tokens: 100,
    temperature: 0.8,
    messages: [
      { role: 'system', content: 'あなたはMyAIパーソナルコンサルタントです。診断の途中で、ユーザーに短いカジュアルなフィードバックを返してください。絵文字を1つだけ使ってOK。' },
      { role: 'user', content: insightPrompt },
    ],
  });
  res.json({ insight: completion.choices[0]?.message?.content?.trim() || '' });
});
