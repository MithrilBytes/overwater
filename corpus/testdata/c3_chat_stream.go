package web

import (
	"context"

	"github.com/anthropics/anthropic-sdk-go"
)

// ChatTurn streams the next assistant reply for the onboarding widget.
func ChatTurn(ctx context.Context, client anthropic.Client, history []anthropic.MessageParam) (anthropic.Message, error) {
	stream := client.Messages.NewStreaming(ctx, anthropic.MessageNewParams{
		Model:     "claude-sonnet-5",
		MaxTokens: 800,
		System: []anthropic.TextBlockParam{
			{Text: "You are the onboarding assistant. Keep the conversation friendly and short."},
		},
		Messages: history,
	})
	message := anthropic.Message{}
	for stream.Next() {
		if err := message.Accumulate(stream.Current()); err != nil {
			return message, err
		}
	}
	return message, stream.Err()
}
