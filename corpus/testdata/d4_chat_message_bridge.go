package bridge

import (
	"context"

	"github.com/openai/openai-go"
)

// Bridges a live chat room where the two people do not share a language.
const bridgePrompt = "Put the chat message into the recipient's language. Keep emoji and slang; do not reply to the message."

func Bridge(ctx context.Context, client openai.Client, message, target string) (string, error) {
	completion, err := client.Chat.Completions.New(ctx, openai.ChatCompletionNewParams{
		Model:       "gpt-4o-mini",
		MaxTokens:   openai.Int(400),
		Temperature: openai.Float(0),
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.SystemMessage(bridgePrompt),
			openai.UserMessage(target + "\n" + message),
		},
	})
	if err != nil {
		return "", err
	}
	return completion.Choices[0].Message.Content, nil
}
