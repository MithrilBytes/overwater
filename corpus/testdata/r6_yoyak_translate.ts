// From dahlia/yoyak, src/translate.ts, with the model construction from
// getModel in src/cli.ts and the first entry of the canonical model table in
// src/models.ts, which is where the repository keeps the provider class and
// id. The table's other entries are dropped, and providerModelName, which
// repeats the key for this entry, is folded into it, so one model string is
// left; the model still reaches translate() as a parameter, as it does from
// the CLI. The stream loop that re-prompts until the delimiter shows up is
// verbatim.

import { authoritativeLabels, type LanguageCode } from "@hongminhee/iso639-1";
import { ChatAnthropic } from "@langchain/anthropic";
import {
  AIMessage,
  type BaseMessage,
  HumanMessage,
  SystemMessage,
} from "@langchain/core/messages";
import { getLogger } from "@logtape/logtape";
import { detect } from "tinyld";

const logger = getLogger(["yoyak", "translate"]);

export interface MessageLike {
  content: unknown;
}

export interface ModelLike {
  invoke(...args: unknown[]): Promise<MessageLike>;
  stream(...args: unknown[]): Promise<AsyncIterable<MessageLike>>;
}

export type ModelClass = new (
  ...args: unknown[]
) => ModelLike;

function asModelClass(modelClass: unknown): ModelClass {
  return modelClass as ModelClass;
}

/**
 * The per-model provider configuration.
 */
const modelConfigs = {
  "claude-3-5-haiku-latest": {
    modelClass: asModelClass(ChatAnthropic),
  },
} as const;

export type CanonicalModelMoniker = keyof typeof modelConfigs;

export function getModelClass(model: CanonicalModelMoniker): ModelClass {
  return modelConfigs[model].modelClass;
}

export function getProviderModelName(model: CanonicalModelMoniker): string {
  return model;
}

async function getModel(
  options: { model?: CanonicalModelMoniker; apiKey?: string },
): Promise<ModelLike> {
  let { model, apiKey } = options;
  const settings = await loadSettings();
  model ??= settings?.model ?? undefined;
  apiKey ??= settings?.apiKey ?? undefined;
  if (model == null) {
    throw new Error("-m/--model: The model must be specified for translation.");
  }
  if (apiKey == null) {
    throw new Error("-a/--api-key: The API key must be specified for the model.");
  }
  const ModelClass = getModelClass(model);
  return new ModelClass({
    model: getProviderModelName(model),
    apiKey,
  });
}

function getSystemPrompt(language: LanguageCode, delimiter: string): string {
  const languageName = authoritativeLabels[language].en;
  return `You are a highly skilled translator with expertise in many languages. \
Your task is to identify the language of the text I provide and accurately translate \
it into the ${languageName} language while preserving the meaning, tone, \
and nuance of the original text. Please maintain proper grammar, spelling, \
and punctuation in the translated version. The input and output are both in Markdown. \
No other information is needed than the text itself.

After you finish your translation, append the following marker: "${delimiter}" \
(without quotes). This marker indicates that your translation is complete.`;
}

const CONTINUE_PROMPT = `Please continue translating right after the previous \
translation, without any duplicate sentences.`;

/**
 * Options for {@link translate} function.
 */
export interface TranslateOptions {
  /**
   * An optional signal to cancel the operation.
   */
  signal?: AbortSignal;
}

/**
 * Translates the given text into the target language.
 * @param model The model to use for translation.
 * @param text The text to translate.
 * @param targetLanguage The target language code in ISO 639-1.
 * @param options The options for translation.
 * @returns The translated text.
 */
export async function* translate(
  model: ModelLike,
  text: string,
  targetLanguage: LanguageCode,
  options: TranslateOptions = {},
): AsyncIterable<string> {
  if (detect(text) === targetLanguage) {
    yield text;
    return;
  }
  const delimiter = `</${Math.random().toString(36).substring(2, 10)}>`;
  const messages: BaseMessage[] = [
    new SystemMessage(getSystemPrompt(targetLanguage, delimiter)),
    new HumanMessage(text),
  ];
  let complete = false;
  do {
    logger.debug("Invoking the model with messages: {messages}", { messages });
    const result = await model.stream(messages, { signal: options.signal });
    logger.debug("Received the result: {result}", { result });
    let message = "";
    let buffer = "";
    for await (const chunk of result) {
      const str = getMessageText(chunk);
      buffer += str;
      message += str;
      if (buffer.match(/<\/[0-9a-z]{0,10}$/)) continue;
      complete = message.includes(delimiter);
      if (complete) {
        buffer = buffer.substring(0, buffer.indexOf(delimiter));
      }
      yield buffer;
      buffer = "";
      if (complete) break;
    }
    if (!complete) yield " ";
    messages.push(
      new AIMessage(message),
      new HumanMessage(CONTINUE_PROMPT),
    );
  } while (!complete);
}

export async function translateCommand(
  text: string,
  language: LanguageCode,
  options: { model?: CanonicalModelMoniker; apiKey?: string },
  signal?: AbortSignal,
): Promise<void> {
  const model = await getModel(options);
  for await (const chunk of translate(model, text, language, { signal })) {
    await Deno.stdout.write(new TextEncoder().encode(chunk));
  }
}

declare function loadSettings(): Promise<{ model?: CanonicalModelMoniker; apiKey?: string } | undefined>;
declare function getMessageText(message: MessageLike): string;
declare const Deno: { stdout: { write(bytes: Uint8Array): Promise<number> } };
