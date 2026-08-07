package vision

import (
	"context"

	"github.com/openai/openai-go"
)

// Answers a question about an uploaded picture.
func Ask(ctx context.Context, client openai.Client, imageURL, question string) (string, error) {
	completion, err := client.Chat.Completions.New(ctx, openai.ChatCompletionNewParams{
		Model:     "gpt-4o",
		MaxTokens: openai.Int(400),
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.UserMessageParts(
				openai.TextContentPart(question),
				openai.ImageContentPart(openai.ChatCompletionContentPartImageImageURLParam{URL: imageURL}),
			),
		},
	})
	if err != nil {
		return "", err
	}
	return completion.Choices[0].Message.Content, nil
}
