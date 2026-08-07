package stt

import (
	"context"
	"os"

	"github.com/openai/openai-go"
)

func Transcribe(ctx context.Context, client openai.Client, f *os.File) (string, error) {
	result, err := client.Audio.Transcriptions.New(ctx, openai.AudioTranscriptionNewParams{
		Model:    "gpt-4o-mini",
		File:     f,
		Language: openai.String("en"),
	})
	if err != nil {
		return "", err
	}
	return result.Text, nil
}
