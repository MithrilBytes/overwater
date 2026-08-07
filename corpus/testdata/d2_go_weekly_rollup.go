package rollup

import (
	"context"

	"github.com/openai/openai-go"
)

const rollupPrompt = "Roll the week's shipped work into a five sentence update for the leadership channel. Lead with impact, not activity."

func Weekly(ctx context.Context, client openai.Client, entries string) (string, error) {
	completion, err := client.Chat.Completions.New(ctx, openai.ChatCompletionNewParams{
		Model:     "gpt-4.1",
		MaxTokens: openai.Int(600),
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.SystemMessage(rollupPrompt),
			openai.UserMessage(entries),
		},
	})
	if err != nil {
		return "", err
	}
	return completion.Choices[0].Message.Content, nil
}
