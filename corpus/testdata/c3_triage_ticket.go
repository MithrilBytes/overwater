package support

import (
	"context"

	"github.com/anthropics/anthropic-sdk-go"
)

// TriageTicket labels an inbound support ticket so routing can queue it.
func TriageTicket(ctx context.Context, client anthropic.Client, body string) (string, error) {
	msg, err := client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     "claude-haiku-4-5",
		MaxTokens: 50,
		System: []anthropic.TextBlockParam{
			{Text: "Categorize the ticket as billing, bug, account, or other. Reply with the label only."},
		},
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(body)),
		},
	})
	if err != nil {
		return "", err
	}
	return msg.Content[0].Text, nil
}
