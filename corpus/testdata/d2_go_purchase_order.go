package ingest

import (
	"context"

	"github.com/openai/openai-go"
)

const poPrompt = "Pull the vendor, po_number, line_item_count, and total from the purchase order. Return JSON with those keys only."

func ReadPurchaseOrder(ctx context.Context, client openai.Client, doc string) (string, error) {
	completion, err := client.Chat.Completions.New(ctx, openai.ChatCompletionNewParams{
		Model:       "gpt-4.1-mini",
		MaxTokens:   openai.Int(400),
		Temperature: openai.Float(0),
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.SystemMessage(poPrompt),
			openai.UserMessage(doc),
		},
	})
	if err != nil {
		return "", err
	}
	return completion.Choices[0].Message.Content, nil
}
