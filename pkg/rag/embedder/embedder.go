package embedder

import (
	"context"
)

// Client is the minimal client capability required by Wrapper.
// It is satisfied by pkg/llm/openai.EmbeddingClient.
type Client interface {
	Embed(ctx context.Context, texts []string) ([][]float32, error)
	Dim() int
}

type Wrapper struct {
	client Client
}

func NewWrapper(client Client) *Wrapper {
	return &Wrapper{client: client}
}

func (e *Wrapper) EmbedDocuments(ctx context.Context, texts []string) ([][]float32, error) {
	return e.client.Embed(ctx, texts)
}

func (e *Wrapper) EmbedQuery(ctx context.Context, text string) ([]float32, error) {
	vectors, err := e.client.Embed(ctx, []string{text})
	if err != nil {
		return nil, err
	}
	if len(vectors) == 0 {
		return nil, nil
	}
	return vectors[0], nil
}

func (e *Wrapper) Dim() int {
	if e == nil || e.client == nil {
		return 0
	}
	return e.client.Dim()
}
