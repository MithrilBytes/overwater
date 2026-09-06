// From angelnicolasc/graymatter: pkg/embedding/openai.go. The call is a
// raw HTTP POST to the embeddings endpoint; the model comes from
// Config.OpenAIModel with the package constant as the fallback. The LRU
// cache is kept because Embed reads through it.

package embedding

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

const (
	openaiEmbedURL = "https://api.openai.com/v1/embeddings"
	openaiModel    = "text-embedding-3-small"
	openaiDims     = 1536
	cacheSize      = 512
)

// Config carries the provider settings the embedding package reads.
type Config struct {
	OpenAIAPIKey string
	OpenAIModel  string
}

// OpenAIProvider calls the OpenAI Embeddings API (text-embedding-3-small by default).
// It maintains a small in-process LRU cache to avoid re-embedding identical texts.
type OpenAIProvider struct {
	apiKey     string
	model      string
	httpClient *http.Client
	mu         sync.Mutex
	cache      map[string][]float32
	cacheOrder []string
}

// NewOpenAI creates an OpenAI-backed embedding provider.
// If cfg.OpenAIModel is empty, it defaults to text-embedding-3-small.
func NewOpenAI(cfg Config) *OpenAIProvider {
	model := cfg.OpenAIModel
	if model == "" {
		model = openaiModel
	}
	return &OpenAIProvider{
		apiKey: cfg.OpenAIAPIKey,
		model:  model,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		cache:      make(map[string][]float32, cacheSize),
		cacheOrder: make([]string, 0, cacheSize),
	}
}

type openaiEmbedRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

type openaiEmbedResponse struct {
	Data []struct {
		Embedding []float32 `json:"embedding"`
	} `json:"data"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

func (o *OpenAIProvider) Embed(ctx context.Context, text string) ([]float32, error) {
	o.mu.Lock()
	if emb, ok := o.cache[text]; ok {
		o.mu.Unlock()
		return emb, nil
	}
	o.mu.Unlock()

	body, err := json.Marshal(openaiEmbedRequest{
		Model: o.model,
		Input: []string{text},
	})
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, openaiEmbedURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+o.apiKey)

	resp, err := o.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("openai embed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body := errorBody(resp.Body)
		return nil, fmt.Errorf("openai embed: status %d: %s", resp.StatusCode, body)
	}

	var result openaiEmbedResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("openai embed decode: %w", err)
	}
	if result.Error != nil {
		return nil, fmt.Errorf("openai embed: %s", result.Error.Message)
	}
	if len(result.Data) == 0 {
		return nil, fmt.Errorf("openai embed: empty response")
	}

	emb := result.Data[0].Embedding
	o.store(text, emb)
	return emb, nil
}

func (o *OpenAIProvider) store(text string, emb []float32) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if len(o.cacheOrder) >= cacheSize {
		oldest := o.cacheOrder[0]
		o.cacheOrder = o.cacheOrder[1:]
		delete(o.cache, oldest)
	}
	o.cache[text] = emb
	o.cacheOrder = append(o.cacheOrder, text)
}

// Dims reports the vector width so the store can size its index.
func (o *OpenAIProvider) Dims() int { return openaiDims }

func errorBody(r io.Reader) string {
	b, _ := io.ReadAll(io.LimitReader(r, 4096))
	return string(b)
}
