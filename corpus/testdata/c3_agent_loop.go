package ops

import (
	"context"

	"github.com/anthropics/anthropic-sdk-go"
)

var searchRunbooks = anthropic.ToolParam{
	Name:        "search_runbooks",
	Description: anthropic.String("Search the runbook wiki and return matching pages."),
	InputSchema: anthropic.ToolInputSchemaParam{
		Properties: map[string]interface{}{
			"query": map[string]interface{}{"type": "string"},
		},
	},
}

func RunIncidentTurn(ctx context.Context, client anthropic.Client, history []anthropic.MessageParam) (*anthropic.Message, error) {
	return client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     "claude-opus-5",
		MaxTokens: 2048,
		Tools:     []anthropic.ToolUnionParam{{OfTool: &searchRunbooks}},
		Messages:  history,
	})
}
