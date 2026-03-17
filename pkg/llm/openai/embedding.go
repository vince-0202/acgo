package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"acgo/pkg/keys"
	"acgo/pkg/log"
)

// EmbeddingClient is a minimal client for the OpenAI /embeddings API.
// It is kept separate from chat completions so it can be used by pkg/rag
// without depending on llm.Provider interfaces.
type EmbeddingClient struct {
	baseURL    string
	apiKey     string
	model      string
	httpClient *http.Client
}

// NewEmbeddingClient creates a new EmbeddingClient with the given model ID.
// baseURL should be the same root as used for Chat Completions (e.g. https://api.openai.com/v1).
func NewEmbeddingClient(baseURL, apiKey, model string, httpClient *http.Client) *EmbeddingClient {
	if httpClient == nil {
		httpClient = &http.Client{}
	}
	return &EmbeddingClient{
		baseURL:    baseURL,
		apiKey:     apiKey,
		model:      model,
		httpClient: httpClient,
	}
}

type embeddingRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

type embeddingResponse struct {
	Data []struct {
		Embedding []float32 `json:"embedding"`
	} `json:"data"`
}

// Embed computes embeddings for the given input texts.
func (c *EmbeddingClient) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}
	log.Debugf("[embedding] Embed: model=%s input_count=%d", c.model, len(texts))
	body, err := json.Marshal(embeddingRequest{
		Model: c.model,
		Input: texts,
	})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set(keys.ContentType, "application/json")
	req.Header.Set(keys.Authorization, "Bearer "+c.apiKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		log.Debugf("[embedding] Embed: request err=%v", err)
		return nil, err
	}
	defer resp.Body.Close()

	log.Debugf("[embedding] Embed: status=%d", resp.StatusCode)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("openai embeddings API %d", resp.StatusCode)
	}

	var out embeddingResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}

	vectors := make([][]float32, 0, len(out.Data))
	for _, d := range out.Data {
		vectors = append(vectors, d.Embedding)
	}
	if len(vectors) > 0 {
		log.Debugf("[embedding] Embed: returned count=%d dim=%d", len(vectors), len(vectors[0]))
	}
	return vectors, nil
}

// Dim returns the embedding dimension if known. It issues a tiny request
// with one token input when the dimension has not been observed yet.
func (c *EmbeddingClient) Dim() int {
	// Lazy detection is more complex (requires caching and a context),
	// so for now callers treat 0 as \"unknown\" and rely on the first
	// successful Embed call to infer dimension from the returned vectors.
	return 0
}
