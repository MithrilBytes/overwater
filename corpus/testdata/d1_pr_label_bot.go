package labeler

import (
	"context"
	"strings"

	"github.com/openai/openai-go"
)

const rubric = "Pick one label for the pull request: feature, fix, chore, or docs. Answer with the label only."

func LabelPR(ctx context.Context, client openai.Client, diff string) (string, error) {
	completion, err := client.Chat.Completions.New(ctx, openai.ChatCompletionNewParams{
		Model:       "gpt-4.1-nano",
		MaxTokens:   openai.Int(8),
		Temperature: openai.Float(0),
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.SystemMessage(rubric),
			openai.UserMessage(diff),
		},
	})
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(completion.Choices[0].Message.Content), nil
}
