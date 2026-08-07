package guard

import (
	"context"
	"strings"

	"github.com/openai/openai-go"
)

const policy = "You screen in game chat. Block slurs, threats, and account trading offers. Reply ALLOW or BLOCK."

func Allowed(ctx context.Context, client openai.Client, line string) (bool, error) {
	completion, err := client.Chat.Completions.New(ctx, openai.ChatCompletionNewParams{
		Model:       "gpt-4.1-nano",
		MaxTokens:   openai.Int(3),
		Temperature: openai.Float(0),
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.SystemMessage(policy),
			openai.UserMessage(line),
		},
	})
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(completion.Choices[0].Message.Content) == "ALLOW", nil
}
