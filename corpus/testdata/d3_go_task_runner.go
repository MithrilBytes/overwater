package agent

import (
	"context"

	"github.com/openai/openai-go"
)

// Step runs one turn of the task loop; scratchpad already holds the
// tool results from previous turns.
func Step(ctx context.Context, client openai.Client, scratchpad []openai.ChatCompletionMessageParamUnion, tools []openai.ChatCompletionToolParam) (*openai.ChatCompletion, error) {
	return client.Chat.Completions.New(ctx, openai.ChatCompletionNewParams{
		Model:     "gpt-5.1",
		Messages:  scratchpad,
		Tools:     tools,
		MaxTokens: openai.Int(2500),
	})
}
