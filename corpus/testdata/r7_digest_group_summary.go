// From eleith/miniflux-digest: internal/llm/gemini.go, with the summary
// prompts and response schema from internal/digest/view/ai_prompts.go and
// getSummaryForGroup from internal/digest/view/ai.go, where the prompt is
// assembled. The grouping prompts and the chunk worker are dropped; the
// summary of summaries reduce step is kept.

package digest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"google.golang.org/genai"
)

const (
	Model                 = "gemini-2.5-flash"
	maxRetries            = 3
	perTryTimeout         = 5 * time.Minute
	Temperature   float32 = 0.4
)

const (
	MaxEntryContentLengthForLLM = 250
	MaxEntriesPerJob            = 150
)

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

const summaryPrompt = `You are an expert at summarizing content for busy readers. You will be given a list of entries from a primary group titled '%s'.

Your task is to write a concise, 2-3 sentence summary (under 250 words) that achieves two goals:
1.  **Provide a high-level overview** of the main themes in the group.
2.  **Highlight the most significant or surprising entries.** This could be a major announcement, a controversial opinion, or a particularly popular discussion.

The goal is to give the reader enough information to quickly decide if this group contains entries they want to explore further. Do not just list the topics; provide some insight into the content.
`

const summaryOfSummariesPrompt = `You are an expert at creating overviews of large swaths of web updates and web content.

Your task is to write a concise, 2-3 sentence summary (under 250 words) that achieves two goals:
1.  **Provide a high-level overview** of the main themes.
2.  **Highlight the most significant or surprising entries.** This could be a major announcement, a controversial opinion, or a particularly popular discussion.

The goal is to give the reader enough information to quickly decide if this group contains entries they want to explore further. Do not just list the topics; provide some insight into the content.

Here are the summaries to combine:
----------------------------
%s
`

type SummaryResponse struct {
	Summary string `json:"summary"`
}

var SummaryResponseSchema = &genai.Schema{
	Type: genai.TypeObject,
	Properties: map[string]*genai.Schema{
		"summary": {
			Type: genai.TypeString,
		},
	},
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

func getSummaryForGroup(ctx context.Context, groupTitle string, entries []*Entry, llmService *GeminiService) (string, error) {
	var partialSummaries []string
	var failedItems []*Entry

	for start := 0; start < len(entries); start += MaxEntriesPerJob {
		end := start + MaxEntriesPerJob
		if end > len(entries) {
			end = len(entries)
		}
		chunk := entries[start:end]

		llmEntriesJSON, err := json.MarshalIndent(prepareLLMEntries(chunk), "", "  ")
		if err != nil {
			failedItems = append(failedItems, chunk...)
			continue
		}
		prompt := fmt.Sprintf(summaryPrompt, groupTitle) + string(llmEntriesJSON)

		var resp SummaryResponse
		if err := llmService.GenerateContentWithResponse(ctx, prompt, SummaryResponseSchema, &resp); err != nil {
			failedItems = append(failedItems, chunk...)
			continue
		}
		partialSummaries = append(partialSummaries, resp.Summary)
	}

	if len(partialSummaries) == 0 {
		return "", fmt.Errorf("all summary chunks failed for group '%s'", groupTitle)
	}

	// Reduce step: summarize the summaries
	summariesString := strings.Join(partialSummaries, "\n\n---\n\n")
	finalSummaryPromptStr := fmt.Sprintf(summaryOfSummariesPrompt, summariesString)

	var finalSummaryResponse SummaryResponse
	if err := llmService.GenerateContentWithResponse(ctx, finalSummaryPromptStr, SummaryResponseSchema, &finalSummaryResponse); err != nil {
		return "", fmt.Errorf("summary of summaries failed: %w", err)
	}

	finalSummary := finalSummaryResponse.Summary
	if len(failedItems) > 0 {
		finalSummary += fmt.Sprintf("\n\n(Note: %d entries could not be included in the summary due to processing errors.)", len(failedItems))
	}

	return finalSummary, nil
}
