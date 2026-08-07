package bot

import (
	"context"

	"github.com/openai/openai-go"
)

// Handles one message in a running Discord thread.
func Respond(ctx context.Context, client openai.Client, turns []openai.ChatCompletionMessageParamUnion) (string, error) {
	completion, err := client.Chat.Completions.New(ctx, openai.ChatCompletionNewParams{
		Model:     "gpt-4.1-mini",
		Messages:  turns,
		MaxTokens: openai.Int(700),
	})
	if err != nil {
		return "", err
	}
	return completion.Choices[0].Message.Content, nil
}
