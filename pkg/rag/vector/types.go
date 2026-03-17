package vector

import (
	"context"
)

// Record is the payload written to / read from the vector store.
type Record struct {
	ID       string
	Vector   []float32
	Text     string
	Metadata map[string]any
}

// SearchResult is a vector store search hit.
type SearchResult struct {
	Record Record
	Score  float32
}

// Store is an abstract vector database.
type Store interface {
	Upsert(ctx context.Context, records []Record) error
	Search(ctx context.Context, query []float32, topK int, filters map[string]any) ([]SearchResult, error)
	DeleteByDocID(ctx context.Context, ids []string) error
}
