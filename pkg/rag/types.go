package rag

import (
	"context"
	"github.com/vince-0202/acgo/pkg/rag/ingest"
)

// Retriever turns a natural-language query into relevant chunks.
type Retriever interface {
	Retrieve(ctx context.Context, query string, topK int, filters map[string]any) ([]ingest.DocumentChunk, error)
}
