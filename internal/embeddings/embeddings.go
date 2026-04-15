// Package embeddings provides a minimal client for text embedding APIs.
package embeddings

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

const errBodySnippetMax = 256

// Embedder turns text into a dense vector.
type Embedder interface {
	Embed(ctx context.Context, text string) ([]float32, error)
}

const openAIModel = "text-embedding-3-small"

// OpenAIEmbedder calls the OpenAI embeddings REST API.
type OpenAIEmbedder struct {
	apiKey     string
	baseURL    string
	httpClient *http.Client
}

// NewOpenAIEmbedder constructs an embedder. baseURL defaults to the public
// OpenAI endpoint when empty; httpClient defaults to http.DefaultClient.
func NewOpenAIEmbedder(apiKey, baseURL string, httpClient *http.Client) *OpenAIEmbedder {
	if baseURL == "" {
		baseURL = "https://api.openai.com"
	}
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &OpenAIEmbedder{apiKey: apiKey, baseURL: baseURL, httpClient: httpClient}
}

func (e *OpenAIEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	body, err := json.Marshal(map[string]string{
		"model": openAIModel,
		"input": text,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal embeddings request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.baseURL+"/v1/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build embeddings request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+e.apiKey)

	resp, err := e.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("call embeddings API: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, errBodySnippetMax))
		return nil, fmt.Errorf("embeddings API returned %d: %s", resp.StatusCode, string(snippet))
	}

	var out struct {
		Data []struct {
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("decode embeddings response: %w", err)
	}
	if len(out.Data) == 0 {
		return nil, fmt.Errorf("embeddings response contained no data")
	}
	return out.Data[0].Embedding, nil
}
