package rag

import "context"

// EmbeddingClient is the minimal client capability required by OpenAIEmbedder.
// It is satisfied by pkg/llm/openai.EmbeddingClient.
type EmbeddingClient interface {
	Embed(ctx context.Context, texts []string) ([][]float32, error)
	Dim() int
}

type OpenAIEmbedder struct {
	client EmbeddingClient
}

func NewOpenAIEmbedder(client EmbeddingClient) *OpenAIEmbedder {
	return &OpenAIEmbedder{client: client}
}

func (e *OpenAIEmbedder) EmbedDocuments(ctx context.Context, texts []string) ([][]float32, error) {
	return e.client.Embed(ctx, texts)
}

func (e *OpenAIEmbedder) EmbedQuery(ctx context.Context, text string) ([]float32, error) {
	vectors, err := e.client.Embed(ctx, []string{text})
	if err != nil {
		return nil, err
	}
	if len(vectors) == 0 {
		return nil, nil
	}
	return vectors[0], nil
}

func (e *OpenAIEmbedder) Dim() int {
	if e == nil || e.client == nil {
		return 0
	}
	return e.client.Dim()
}
