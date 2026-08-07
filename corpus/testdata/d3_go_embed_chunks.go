package index

import (
	"context"

	"github.com/openai/openai-go"
)

func EmbedChunks(ctx context.Context, client openai.Client, chunks []string) ([][]float64, error) {
	response, err := client.Embeddings.New(ctx, openai.EmbeddingNewParams{
		Model: "text-embedding-3-large",
		Input: openai.EmbeddingNewParamsInputUnion{OfArrayOfStrings: chunks},
	})
	if err != nil {
		return nil, err
	}
	out := make([][]float64, len(response.Data))
	for i, d := range response.Data {
		out[i] = d.Embedding
	}
	return out, nil
}
