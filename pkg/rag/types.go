package rag

import (
	"acgo/pkg/rag/ingest"
	"context"
)

// Retriever turns a natural-language query into relevant chunks.
type Retriever interface {
	Retrieve(ctx context.Context, query string, topK int, filters map[string]any) ([]ingest.DocumentChunk, error)
}
