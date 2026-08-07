package rewrite

import (
	"context"

	"github.com/openai/openai-go"
)

const patchPrompt = "Rewrite the file to use the new context aware API. Return the whole file, no diff markers, no prose."

func Patch(ctx context.Context, client openai.Client, file string) (string, error) {
	completion, err := client.Chat.Completions.New(ctx, openai.ChatCompletionNewParams{
		Model:       "gpt-5.1",
		MaxTokens:   openai.Int(4000),
		Temperature: openai.Float(0.1),
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.SystemMessage(patchPrompt),
			openai.UserMessage(file),
		},
	})
	if err != nil {
		return "", err
	}
	return completion.Choices[0].Message.Content, nil
}
