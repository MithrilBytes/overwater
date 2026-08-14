// From hexlet-basics/hexlet-basics: internal/assistant/assistant.go and
// internal/lessonreviews/lessonreviews.go, with the OPENAI_MODEL default
// from internal/config/config.go, which is where the repository keeps it.
// The prompt is Russian in the original and is kept that way.

package lessonreviews

import (
	"context"

	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
)

type Config struct {
	OpenAIAccessToken string `env:"OPENAI_ACCESS_TOKEN"`
	OpenAIModel       string `env:"OPENAI_MODEL" envDefault:"gpt-4o-mini"`
}

// OpenAI is the thin completion adapter over the official SDK.
type OpenAI struct {
	client openai.Client
	model  openai.ChatModel
}

func NewOpenAI(token, model string) *OpenAI {
	return &OpenAI{
		client: openai.NewClient(option.WithAPIKey(token)),
		model:  openai.ChatModel(model),
	}
}

// Complete runs a single system+user chat completion and returns the text.
func (c *OpenAI) Complete(ctx context.Context, instructions, prompt string) (string, error) {
	resp, err := c.client.Chat.Completions.New(ctx, openai.ChatCompletionNewParams{
		Model: c.model,
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.SystemMessage(instructions),
			openai.UserMessage(prompt),
		},
	})
	if err != nil {
		return "", err
	}
	return resp.Choices[0].Message.Content, nil
}

// reviewInstructions is the legacy ReviewLessonJob prompt, verbatim: the
// summaries are content-ops material read by Russian-speaking staff, so the
// prompt language is intentional, not tied to the lesson's locale.
const reviewInstructions = `Проанализируй вопросы, которые задают студенты ассистенту по уроку. Вопросы будут переданы ниже.
Суммируй основные претензиии и пожелания. Предложи как поменять урок.`

func ReviewLesson(ctx context.Context, llm *OpenAI, lesson, questions string) (string, error) {
	return llm.Complete(ctx, reviewInstructions, "Урок (теория и упражнение): "+lesson+"\n\nВопросы пользователей: "+questions)
}
