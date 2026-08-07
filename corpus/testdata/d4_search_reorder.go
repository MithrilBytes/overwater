package search

import (
	"context"

	"github.com/openai/openai-go"
)

const orderPrompt = "Reorder the candidate results so the ones that answer the query come first. Reply with the ids in order, comma separated."

func Reorder(ctx context.Context, client openai.Client, query, candidates string) (string, error) {
	completion, err := client.Chat.Completions.New(ctx, openai.ChatCompletionNewParams{
		Model:       "gpt-4o-mini",
		MaxTokens:   openai.Int(100),
		Temperature: openai.Float(0),
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.SystemMessage(orderPrompt),
			openai.UserMessage("query: " + query + "\n" + candidates),
		},
	})
	if err != nil {
		return "", err
	}
	return completion.Choices[0].Message.Content, nil
}
