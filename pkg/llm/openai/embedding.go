package openai

import (
	"context"
	"fmt"
	"net/http"

	oai "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
)

// EmbeddingClient is a minimal client for the OpenAI embeddings API,
// implemented with the official `openai-go` SDK.
//
// It is kept separate from llm.Provider implementations so it can be reused
// by pkg/rag without depending on llm.Provider interfaces.
type EmbeddingClient struct {
	oai   oai.Client
	model string
}

// NewEmbeddingClient creates a new EmbeddingClient for the given embedding model.
// baseURL should be the same root used by chat requests (e.g. https://api.openai.com/v1).
func NewEmbeddingClient(baseURL, apiKey, model string, httpClient *http.Client) *EmbeddingClient {
	opts := []option.RequestOption{
		option.WithBaseURL(baseURL),
	}
	if apiKey != "" {
		opts = append(opts, option.WithAPIKey(apiKey))
	}
	if httpClient != nil {
		opts = append(opts, option.WithHTTPClient(httpClient))
	}

	return &EmbeddingClient{
		oai:   oai.NewClient(opts...),
		model: model,
	}
}

// Embed computes embeddings for the given input texts.
func (c *EmbeddingClient) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}

	resp, err := c.oai.Embeddings.New(ctx, oai.EmbeddingNewParams{
		Model: oai.EmbeddingModel(c.model),
		Input: oai.EmbeddingNewParamsInputUnion{
			OfArrayOfStrings: texts,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("openai embeddings API: %w", err)
	}

	vectors := make([][]float32, 0, len(resp.Data))
	for _, d := range resp.Data {
		vec := make([]float32, 0, len(d.Embedding))
		for _, f := range d.Embedding {
			vec = append(vec, float32(f))
		}
		vectors = append(vectors, vec)
	}
	return vectors, nil
}

// Dim returns the embedding dimension if known.
func (c *EmbeddingClient) Dim() int { return 0 }

/*
package openai

import (
	"context"
	"fmt"
	"net/http"

	oai "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
)

// EmbeddingClient is a minimal client for the OpenAI embeddings API,
// implemented with the official `openai-go` SDK.
type EmbeddingClient struct {
	oai   *oai.Client
	model string
}

// NewEmbeddingClient creates a new EmbeddingClient for the given embedding model.
// baseURL should be the same root used by chat requests (e.g. https://api.openai.com/v1).
func NewEmbeddingClient(baseURL, apiKey, model string, httpClient *http.Client) *EmbeddingClient {
	opts := []option.RequestOption{
		option.WithBaseURL(baseURL),
	}
	if apiKey != "" {
		opts = append(opts, option.WithAPIKey(apiKey))
	}
	if httpClient != nil {
		opts = append(opts, option.WithHTTPClient(httpClient))
	}

	return &EmbeddingClient{
		oai:   oai.NewClient(opts...),
		model: model,
	}
}

// Embed computes embeddings for the given input texts.
func (c *EmbeddingClient) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}

	resp, err := c.oai.Embeddings.New(ctx, oai.EmbeddingNewParams{
		Model: oai.EmbeddingModel(c.model),
		Input: oai.EmbeddingNewParamsInputUnion{
			OfArrayOfStrings: texts,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("openai embeddings API: %w", err)
	}

	vectors := make([][]float32, 0, len(resp.Data))
	for _, d := range resp.Data {
		vec := make([]float32, 0, len(d.Embedding))
		for _, f := range d.Embedding {
			vec = append(vec, float32(f))
		}
		vectors = append(vectors, vec)
	}
	return vectors, nil
}

// Dim returns the embedding dimension if known.
func (c *EmbeddingClient) Dim() int { return 0 }

//

package openai

import (
	"context"
	"fmt"
	"net/http"

	oai "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
)

// EmbeddingClient is a minimal client for the OpenAI embeddings API,
// implemented with the official `openai-go` SDK.
//
// It is kept separate from llm.Provider implementations so it can be reused
// by pkg/rag without depending on llm.Provider interfaces.
type EmbeddingClient struct {
	oai   *oai.Client
	model string
}

// NewEmbeddingClient creates a new EmbeddingClient for the given embedding model.
// baseURL should be the same root used by chat requests (e.g. https://api.openai.com/v1).
func NewEmbeddingClient(baseURL, apiKey, model string, httpClient *http.Client) *EmbeddingClient {
	opts := []option.RequestOption{
		option.WithBaseURL(baseURL),
	}
	if apiKey != "" {
		opts = append(opts, option.WithAPIKey(apiKey))
	}
	if httpClient != nil {
		opts = append(opts, option.WithHTTPClient(httpClient))
	}

	return &EmbeddingClient{
		oai:   oai.NewClient(opts...),
		model: model,
	}
}

// Embed computes embeddings for the given input texts.
func (c *EmbeddingClient) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}

	resp, err := c.oai.Embeddings.New(ctx, oai.EmbeddingNewParams{
		Model: oai.EmbeddingModel(c.model),
		Input: oai.EmbeddingNewParamsInputUnion{
			OfArrayOfStrings: texts,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("openai embeddings API: %w", err)
	}

	vectors := make([][]float32, 0, len(resp.Data))
	for _, d := range resp.Data {
		vec := make([]float32, 0, len(d.Embedding))
		for _, f := range d.Embedding {
			vec = append(vec, float32(f))
		}
		vectors = append(vectors, vec)
	}
	return vectors, nil
}

// Dim returns the embedding dimension if known.
// Lazy detection is more complex (requires caching and a context), so
// callers treat 0 as "unknown" for now.
func (c *EmbeddingClient) Dim() int { return 0 }

package openai

import (
	"context"
	"fmt"
	"net/http"

	oai "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
)

// EmbeddingClient is a minimal client for the OpenAI embeddings API,
// implemented with the official `openai-go` SDK.
//
// It is kept separate from llm.Provider implementations so it can be reused
// by pkg/rag without depending on llm.Provider interfaces.
type EmbeddingClient struct {
	oai   *oai.Client
	model string
}

// NewEmbeddingClient creates a new EmbeddingClient for the given embedding model.
// baseURL should be the same root used by chat requests (e.g. https://api.openai.com/v1).
func NewEmbeddingClient(baseURL, apiKey, model string, httpClient *http.Client) *EmbeddingClient {
	if httpClient != nil {
		return &EmbeddingClient{
			oai: oai.NewClient(
				option.WithBaseURL(baseURL),
				option.WithAPIKey(apiKey),
				option.WithHTTPClient(httpClient),
			),
			model: model,
		}
	}

	return &EmbeddingClient{
		oai: oai.NewClient(
			option.WithBaseURL(baseURL),
			option.WithAPIKey(apiKey),
		),
		model: model,
	}
}

// Embed computes embeddings for the given input texts.
func (c *EmbeddingClient) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}

	resp, err := c.oai.Embeddings.New(ctx, oai.EmbeddingNewParams{
		Model: oai.EmbeddingModel(c.model),
		Input: oai.EmbeddingNewParamsInputUnion{
			OfArrayOfStrings: texts,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("openai embeddings API: %w", err)
	}

	vectors := make([][]float32, 0, len(resp.Data))
	for _, d := range resp.Data {
		vec := make([]float32, 0, len(d.Embedding))
		for _, f := range d.Embedding {
			vec = append(vec, float32(f))
		}
		vectors = append(vectors, vec)
	}
	return vectors, nil
}

// Dim returns the embedding dimension if known.
// Lazy detection is more complex (requires caching and a context), so
// callers treat 0 as "unknown" for now.
func (c *EmbeddingClient) Dim() int { return 0 }

*/
