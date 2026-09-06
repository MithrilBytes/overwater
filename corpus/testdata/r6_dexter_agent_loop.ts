// From virattt/dexter, src/agent/agent.ts, with the model factory and the
// streaming call from src/model/llm.ts and the tool policy section of
// buildSystemPrompt from src/agent/prompts.ts, which is where the repository
// keeps them. The repository's default, gpt-5.6-sol, is not in the catalog,
// so a catalog id is substituted. The blocking fallback that repeats the
// call without streaming, the other provider factories, and the concurrent
// tool executor are dropped so one call and one model string are left; the
// tool results are collected inline instead.

import {
  AIMessage,
  AIMessageChunk,
  BaseMessage,
  HumanMessage,
  SystemMessage,
  ToolMessage,
} from '@langchain/core/messages';
import { BaseChatModel } from '@langchain/core/language_models/chat_models';
import { Runnable } from '@langchain/core/runnables';
import { StructuredToolInterface } from '@langchain/core/tools';
import { ChatOpenAI } from '@langchain/openai';

export const DEFAULT_MODEL = 'gpt-5.1';
const DEFAULT_MAX_ITERATIONS = 10;

// Model provider configuration
interface ModelOpts {
  streaming: boolean;
}

function getApiKey(envVar: string): string {
  const apiKey = process.env[envVar];
  if (!apiKey) {
    throw new Error(`[LLM] ${envVar} not found in environment variables`);
  }
  return apiKey;
}

const DEFAULT_FACTORY = (name: string, opts: ModelOpts): BaseChatModel =>
  new ChatOpenAI({
    model: name,
    ...opts,
    apiKey: getApiKey('OPENAI_API_KEY'),
    // GPT-5.6 requires the Responses API when reasoning and function tools are combined.
    useResponsesApi: name.startsWith('gpt-5.6-'),
  });

export function getChatModel(
  modelName: string = DEFAULT_MODEL,
  streaming: boolean = false
): BaseChatModel {
  const opts: ModelOpts = { streaming };
  return DEFAULT_FACTORY(modelName, opts);
}

interface CallLlmWithMessagesOptions {
  model?: string;
  tools?: StructuredToolInterface[];
  signal?: AbortSignal;
}

/**
 * Stream an LLM response as AIMessageChunk objects.
 *
 * Uses LangChain's .stream() method. Chunks can be accumulated via .concat()
 * to progressively build complete tool_calls.
 */
export async function* streamLlmWithMessages(
  messages: BaseMessage[],
  options: CallLlmWithMessagesOptions = {},
): AsyncGenerator<AIMessageChunk, void> {
  const { model = DEFAULT_MODEL, tools, signal } = options;

  const llm = getChatModel(model, true);

  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  let runnable: Runnable<any, any> = llm;

  if (tools && tools.length > 0 && llm.bindTools) {
    runnable = llm.bindTools(tools);
  }

  const invokeOpts = signal ? { signal } : undefined;

  const stream = await runnable.stream(messages, invokeOpts);

  for await (const chunk of stream) {
    yield chunk as AIMessageChunk;
  }
}

export function buildSystemPrompt(
  profile: { label: string; preamble: string },
  toolDescriptions: string,
): string {
  return `You are Dexter, a ${profile.label} assistant with access to research tools.

Current date: ${getCurrentDate()}

${profile.preamble}

## Available Tools

${toolDescriptions}

## Tool Usage Policy

- Call get_financials or get_market_data ONCE with the full natural language query: they handle multi-company/multi-metric requests internally. Do NOT break up queries into multiple calls.
- Only use web_fetch when headlines are insufficient (need quotes, deal specifics, earnings details).
- Tool results are automatically capped. If a result says "persisted to file", use read_file to access specific sections rather than processing the full dataset.
- Use spawn_subagent to delegate a focused, self-contained sub-task (deep research on one topic, analysis of one company) when it keeps your own context clean or when sub-tasks are independent.
- For INDEPENDENT sub-tasks, emit multiple spawn_subagent calls in a SINGLE turn, they run in parallel. Chain across turns only when one sub-task depends on another's output.
- Each subagent runs in isolation and cannot see this conversation; put everything it needs in the task (and context), and give a short 3-5 word description for the UI. It returns one final answer for you to synthesize. Don't delegate trivial single-tool lookups you can do directly.
- Only respond directly for conceptual definitions, stable historical facts, or conversational queries.`;
}

export interface AgentConfig {
  model?: string;
  maxIterations?: number;
  signal?: AbortSignal;
}

export type AgentEvent =
  | { type: 'stream_progress'; charDelta: number; mode: string }
  | { type: 'thinking'; message: string }
  | { type: 'tool_start'; tool: string }
  | { type: 'done'; answer: string; iterations: number };

/**
 * The core agent class that handles the agent loop and tool execution.
 *
 * Architecture:
 * - Growing message array with full reasoning continuity
 * - Streaming LLM responses
 */
export class Agent {
  private readonly model: string;
  private readonly maxIterations: number;
  private readonly tools: StructuredToolInterface[];
  private readonly toolMap: Map<string, StructuredToolInterface>;
  private readonly systemPrompt: string;
  private readonly signal?: AbortSignal;

  constructor(
    config: AgentConfig,
    tools: StructuredToolInterface[],
    systemPrompt: string,
  ) {
    this.model = config.model ?? DEFAULT_MODEL;
    this.maxIterations = config.maxIterations ?? DEFAULT_MAX_ITERATIONS;
    this.tools = tools;
    this.toolMap = new Map(tools.map(t => [t.name, t]));
    this.systemPrompt = systemPrompt;
    this.signal = config.signal;
  }

  async *run(query: string, historyMessages: BaseMessage[] = []): AsyncGenerator<AgentEvent> {
    let messages: BaseMessage[] = [
      new SystemMessage(this.systemPrompt),
      ...historyMessages,
      new HumanMessage(query),
    ];

    // Main agent loop
    let iteration = 0;
    while (iteration < this.maxIterations) {
      iteration++;

      const { response } = yield* this.streamAndAccumulate(messages);

      const responseText = extractTextContent(response);

      // Emit thinking if there are also tool calls
      if (responseText?.trim() && hasToolCalls(response)) {
        yield { type: 'thinking', message: responseText.trim() };
      }

      // No tool calls = final answer
      if (!hasToolCalls(response)) {
        yield { type: 'done', answer: responseText ?? '', iterations: iteration };
        return;
      }

      // Push AIMessage to conversation history
      messages.push(response);

      // Execute tools, collect ToolMessages by ID
      const toolMessages = yield* this.executeToolsAndCollectMessages(response);

      messages.push(...toolMessages);
    }

    // Max iterations reached
    yield {
      type: 'done',
      answer: `Reached maximum iterations (${this.maxIterations}). I was unable to complete the research in the allotted steps.`,
      iterations: iteration,
    };
  }

  /**
   * Stream the LLM response, yielding per-chunk progress events and finally
   * returning the accumulated AIMessage.
   */
  private async *streamAndAccumulate(
    messages: BaseMessage[],
  ): AsyncGenerator<AgentEvent, { response: AIMessage }> {
    yield { type: 'stream_progress', charDelta: 0, mode: 'requesting' };

    let accumulated: AIMessageChunk | null = null;

    for await (const chunk of streamLlmWithMessages(messages, {
      model: this.model,
      tools: this.tools,
      signal: this.signal,
    })) {
      accumulated = accumulated ? accumulated.concat(chunk) : chunk;
      const { charDelta, mode } = inspectChunkContent(chunk);
      if (charDelta > 0 || mode !== 'responding') {
        yield { type: 'stream_progress', charDelta, mode };
      }
    }

    if (!accumulated) {
      throw new Error('Stream produced no chunks');
    }

    const response = new AIMessage({
      content: accumulated.content,
      tool_calls: accumulated.tool_calls,
      invalid_tool_calls: accumulated.invalid_tool_calls,
      usage_metadata: accumulated.usage_metadata,
      response_metadata: accumulated.response_metadata,
    });

    if (response.tool_calls && response.tool_calls.length > 0) {
      yield { type: 'stream_progress', charDelta: 0, mode: 'tool-use' };
    }

    return { response };
  }

  /**
   * Execute tools and collect ToolMessages in ORIGINAL tool_calls order.
   */
  private async *executeToolsAndCollectMessages(
    response: AIMessage,
  ): AsyncGenerator<AgentEvent, ToolMessage[]> {
    const toolMessages: ToolMessage[] = [];

    for (const tc of response.tool_calls!) {
      yield { type: 'tool_start', tool: tc.name };
      const tool = this.toolMap.get(tc.name);
      try {
        const result = tool ? await tool.invoke(tc.args) : `Error: unknown tool ${tc.name}`;
        toolMessages.push(new ToolMessage({
          content: typeof result === 'string' ? result : JSON.stringify(result),
          tool_call_id: tc.id!,
          name: tc.name,
        }));
      } catch (error) {
        toolMessages.push(new ToolMessage({
          content: `Error: ${error instanceof Error ? error.message : String(error)}`,
          tool_call_id: tc.id!,
          name: tc.name,
        }));
      }
    }

    return toolMessages;
  }
}

declare function getCurrentDate(): string;
declare function extractTextContent(message: AIMessage): string | null;
declare function hasToolCalls(message: AIMessage): boolean;
declare function inspectChunkContent(chunk: AIMessageChunk): { charDelta: number; mode: string };
