// From eleith/miniflux-digest: internal/llm/gemini.go, with the strict
// grouping prompt and response schema from
// internal/digest/view/ai_prompts.go and GroupAIEntries from
// internal/digest/view/ai.go, where the prompt template is rendered with
// the user's configured categories. The open-ended grouping branch, the
// consolidation pass, and the chunk worker are dropped.

package digest

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"text/template"
	"time"

	"google.golang.org/genai"
)

const (
	Model                 = "gemini-2.5-flash"
	maxRetries            = 3
	perTryTimeout         = 5 * time.Minute
	Temperature   float32 = 0.4
)

const MaxEntryContentLengthForLLM = 250

type modelClient interface {
	GenerateContent(ctx context.Context, model string, parts []*genai.Content, config *genai.GenerateContentConfig) (*genai.GenerateContentResponse, error)
}

type GeminiService struct {
	client    modelClient
	modelName string
	sleep     func(time.Duration)
}

func NewGeminiService(apiKey string) (*GeminiService, error) {
	if apiKey == "" {
		return &GeminiService{modelName: Model, sleep: time.Sleep}, nil
	}

	ctx := context.Background()
	clientConfig := genai.ClientConfig{APIKey: apiKey}
	client, err := genai.NewClient(ctx, &clientConfig)
	if err != nil {
		return nil, err
	}

	return &GeminiService{client: client.Models, modelName: Model, sleep: time.Sleep}, nil
}

func (s *GeminiService) generateContentWithRetry(ctx context.Context, prompt string, schema *genai.Schema) (*genai.GenerateContentResponse, error) {
	var lastErr error

	for i := 1; i <= maxRetries; i++ {
		tryCtx, tryCancel := context.WithTimeout(ctx, perTryTimeout)
		resp, err := s.client.GenerateContent(tryCtx, s.modelName, genai.Text(prompt), &genai.GenerateContentConfig{
			ResponseMIMEType: "application/json",
			ResponseSchema:   schema,
			Temperature:      genai.Ptr(Temperature),
		})
		tryCancel()

		if err == nil {
			return resp, nil
		}

		lastErr = err

		log.Printf("LLM attempt %d/%d failed: %v", i, maxRetries, err)

		var apiError genai.APIError
		if errors.As(err, &apiError) && (apiError.Code == 503 || apiError.Code == 500) {
			if i < maxRetries {
				log.Printf("Retrying after server error...")
				s.sleep(time.Second * time.Duration(i))
				continue
			}
		}

		break
	}

	return nil, lastErr
}

func (s *GeminiService) GenerateContent(ctx context.Context, prompt string, schema *genai.Schema) ([]byte, error) {
	if s.client == nil {
		return nil, errors.New("LLM service is disabled: no API key provided")
	}

	resp, err := s.generateContentWithRetry(ctx, prompt, schema)
	if err != nil {
		return nil, err
	}

	candidates := resp.Candidates

	if len(candidates) == 0 || len(candidates[0].Content.Parts) == 0 {
		log.Println("LLM: No content returned from LLM (empty candidates or parts).")
		return nil, errors.New("no content returned from LLM")
	}

	candidate := candidates[0]
	part := candidate.Content.Parts[0]
	jsonData := []byte(part.Text)

	if candidate.FinishReason != genai.FinishReasonStop {
		log.Printf("LLM: Unexpected finish reason: %s", candidate.FinishReason)
	}

	return jsonData, nil
}

func (s *GeminiService) GenerateContentWithResponse(ctx context.Context, prompt string, schema *genai.Schema, response interface{}) error {
	llmResponseBytes, err := s.GenerateContent(ctx, prompt, schema)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(llmResponseBytes, &response); err != nil {
		return fmt.Errorf("failed to parse LLM response: %w", err)
	}
	return nil
}

const strictGroupingPrompt = `You are organizing a digest into specific categories.

**Your Task:**
Sort the entries below into the following categories:
{{range .}}
- **{{.Title}}**: {{.Description}}
{{end}}

**Rules:**
- You MUST use ONLY the categories listed above.
- Do NOT create new categories.
- If an entry does not fit well into any category, do NOT include it in a group (it will be handled as uncategorized later).
- An entry can only belong to one group.

Below is the list of entries:
----------------------------
`

type InitialGroupingResponse struct {
	Groups []struct {
		Title    string  `json:"title"`
		EntryIDs []int64 `json:"entry_ids"`
	} `json:"groups"`
}

var InitialGroupingResponseSchema = &genai.Schema{
	Type: genai.TypeObject,
	Properties: map[string]*genai.Schema{
		"groups": {
			Type: genai.TypeArray,
			Items: &genai.Schema{
				Type: genai.TypeObject,
				Properties: map[string]*genai.Schema{
					"title": {
						Type: genai.TypeString,
					},
					"entry_ids": {
						Type: genai.TypeArray,
						Items: &genai.Schema{
							Type: genai.TypeInteger,
						},
					},
				},
			},
		},
	},
}

// ConfigCategory is one user-declared digest category from the config file.
type ConfigCategory struct {
	Title       string `koanf:"title" validate:"required"`
	Description string `koanf:"description"`
}

type Entry struct {
	ID        int64
	Title     string
	URL       string
	Content   string
	FeedTitle string
}

type llmEntry struct {
	ID        int64  `json:"id"`
	Title     string `json:"title"`
	URL       string `json:"url"`
	Content   string `json:"content"`
	FeedTitle string `json:"feed_title"`
}

func prepareLLMEntries(entries []*Entry) []llmEntry {
	var llmEntries []llmEntry
	for _, entry := range entries {
		content := entry.Content
		if len(content) > MaxEntryContentLengthForLLM {
			content = content[:MaxEntryContentLengthForLLM]
		}
		llmEntries = append(llmEntries, llmEntry{
			ID:        entry.ID,
			Title:     entry.Title,
			URL:       entry.URL,
			Content:   content,
			FeedTitle: entry.FeedTitle,
		})
	}
	return llmEntries
}

func GroupAIEntries(ctx context.Context, entries []*Entry, llmService *GeminiService, categories []ConfigCategory) (map[string][]*Entry, map[int64]bool, error) {
	// Prepare for processing
	entryMap := make(map[int64]*Entry)
	for _, entry := range entries {
		entryMap[entry.ID] = entry
	}

	promptTemplate, err := template.New("grouping").Parse(strictGroupingPrompt)
	if err != nil {
		return nil, nil, fmt.Errorf("prompt template parsing failed: %w", err)
	}

	var prompt bytes.Buffer
	if err := promptTemplate.Execute(&prompt, categories); err != nil {
		return nil, nil, fmt.Errorf("failed to execute prompt template: %w", err)
	}
	llmEntriesJSON, err := json.MarshalIndent(prepareLLMEntries(entries), "", "  ")
	if err != nil {
		return nil, nil, fmt.Errorf("failed to marshal entries to JSON: %w", err)
	}
	prompt.Write(llmEntriesJSON)

	var response InitialGroupingResponse
	if err := llmService.GenerateContentWithResponse(ctx, prompt.String(), InitialGroupingResponseSchema, &response); err != nil {
		return nil, nil, err
	}

	// Process the results
	rawGroups := make(map[string][]*Entry)
	groupedEntryIDs := make(map[int64]bool)

	for _, group := range response.Groups {
		if _, ok := rawGroups[group.Title]; !ok {
			rawGroups[group.Title] = []*Entry{}
		}
		for _, entryID := range group.EntryIDs {
			if _, exists := groupedEntryIDs[entryID]; exists {
				continue
			}
			if entry, ok := entryMap[entryID]; ok {
				rawGroups[group.Title] = append(rawGroups[group.Title], entry)
				groupedEntryIDs[entryID] = true
			}
		}
	}

	return rawGroups, groupedEntryIDs, nil
}
