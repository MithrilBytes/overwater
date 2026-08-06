package hiring

import (
	"context"

	"github.com/openai/openai-go/v2"
)

func ParseResume(ctx context.Context, client openai.Client, resume string) (string, error) {
	completion, err := client.Chat.Completions.New(ctx, openai.ChatCompletionNewParams{
		Model: "gpt-4o",
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.SystemMessage("Extract the candidate name, email, and last three roles from the resume as JSON."),
			openai.UserMessage(resume),
		},
	})
	if err != nil {
		return "", err
	}
	return completion.Choices[0].Message.Content, nil
}
